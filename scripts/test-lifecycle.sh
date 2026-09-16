#!/bin/sh
# Simulate the fnOS app lifecycle locally (no fnOS required): install, start,
# API probe over the Unix socket, stop, uninstall and state cleanup.
#
# Usage: scripts/test-lifecycle.sh
set -eu

ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
DEV="$(mktemp -d)"
trap 'rm -rf "$DEV"' EXIT

mkdir -p "$DEV/target/bin"
echo "building the daemon into the throwaway tree"
(cd "$ROOT" && CGO_ENABLED=0 go build -tags fnos -o "$DEV/target/bin/easytier-pro-fnos" ./cmd/easytier-pro-fnos)
cp -R "$ROOT/fpk/cmd" "$DEV/cmd"
chmod 755 "$DEV/cmd/"*

export TRIM_APPNAME=easytier-pro
export TRIM_APPVER=0.1.0
export TRIM_APPDEST="$DEV/target"
export TRIM_APPDEST_VOL="$DEV"
export TRIM_PKGVAR="$DEV/var"
export TRIM_PKGETC="$DEV/etc"
export TRIM_PKGTMP="$DEV/tmp"
export TRIM_TEMP_LOGFILE="$DEV/install.log"
# Outside fnOS there is no gateway to inject identity headers, so the test
# uses the development bypass that ETP_DEV_ROOT unlocks.
export ETP_DEV_ROOT="$DEV"
export ETP_DEV_NO_FNOS_AUTH=1

echo "== install_init"
"$DEV/cmd/install_init"
echo "== install_callback"
"$DEV/cmd/install_callback"

echo "== start"
"$DEV/cmd/main" start

# Wait for the daemon to create its socket; on a slow runner the fixed sleep is
# not enough and the API probe races the bind.
sock="$DEV/target/app.sock"
for _ in $(seq 1 50); do
	[ -S "$sock" ] && break
	sleep 0.2
done
[ -S "$sock" ] || { echo "daemon socket did not appear at $sock" >&2; exit 1; }

echo "== status (expect 0)"
"$DEV/cmd/main" status

echo "== API probe"
for _ in $(seq 1 25); do
	if curl -sf --unix-socket "$sock" http://localhost/api/status | grep -q '"ok":true'; then
		break
	fi
	sleep 0.2
done
curl -sf --unix-socket "$sock" http://localhost/api/status | grep -q '"ok":true' || {
	echo "API probe failed" >&2
	exit 1
}
echo "api answered ok"

echo "== stop"
"$DEV/cmd/main" stop
set +e
"$DEV/cmd/main" status
code=$?
set -e
[ "$code" -eq 3 ] || { echo "status exit code after stop: $code (expect 3)" >&2; exit 1; }
echo "status exit code after stop: $code (expect 3)"

echo "== uninstall"
TRIM_APP_STATUS=UNINSTALL "$DEV/cmd/uninstall_init"
TRIM_APP_STATUS=UNINSTALL "$DEV/cmd/uninstall_callback"
[ ! -d "$DEV/var/state" ] || { echo "state tree not removed" >&2; exit 1; }

echo "lifecycle OK"
