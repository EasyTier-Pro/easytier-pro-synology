#!/bin/sh
# Lifecycle smoke test: exercises the package start-stop script against a
# throwaway package tree, without DSM.
#
# Usage: tests/smoke-lifecycle.sh [path/to/easytier-pro-dsm]
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${1:-$ROOT/dist/easytier-pro-dsm}"
ROOT_DIR="$(mktemp -d)"
trap 'rm -rf "$ROOT_DIR"' EXIT

export SYNOPKG_PKGNAME=easytier-pro
export SYNOPKG_PKGVER=1.0.0-0001
export SYNOPKG_PKGDEST="$ROOT_DIR/target"
export SYNOPKG_PKGVAR="$ROOT_DIR/var"
export ETP_DEV_ROOT="$ROOT_DIR"
# Outside DSM there is no authenticate.cgi, so the smoke test uses the
# development bypass that ETP_DEV_ROOT unlocks.
export ETP_DEV_NO_DSM_AUTH=1

mkdir -p "$SYNOPKG_PKGDEST/bin" "$SYNOPKG_PKGVAR"
if [ -f "$BIN" ]; then
	cp "$BIN" "$SYNOPKG_PKGDEST/bin/easytier-pro-dsm"
	chmod 755 "$SYNOPKG_PKGDEST/bin/easytier-pro-dsm"
else
	echo "building the daemon into the throwaway tree"
	(cd "$ROOT" && go build -o "$SYNOPKG_PKGDEST/bin/easytier-pro-dsm" ./cmd/easytier-pro-dsm)
fi

STATUS="$ROOT/spk/scripts/start-stop-status"

echo "== start"
"$STATUS" start
echo "== status (expect 0)"
"$STATUS" status
echo "status exit code: $?"

echo "== API probe"
sleep 1
code="$(curl -s -o "$ROOT_DIR/api.json" -w '%{http_code}' http://127.0.0.1:15890/api/status || true)"
echo "http status: $code"
cat "$ROOT_DIR/api.json" 2>/dev/null || true
echo

echo "== stop"
"$STATUS" stop
set +e
"$STATUS" status
echo "status exit code after stop: $? (expect 3)"
set -e

echo "== state tree"
find "$SYNOPKG_PKGVAR" -maxdepth 2 -type d | sort

echo "smoke test finished"
