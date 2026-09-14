#!/bin/sh
# Prechecks run under a different identity from the lifecycle actions on DSM6.
# They must reject a missing executable without creating runtime state.
set -eu
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work/target/bin"
printf '#!/bin/sh\nexit 1\n' > "$work/target/bin/easytier-pro-dsm"
chmod 755 "$work/target/bin/easytier-pro-dsm"
export SYNOPKG_PKGDEST="$work/target" SYNOPKG_PKGVAR="$work/var"
sh "$ROOT/spk/scripts/start-stop-status" prestart
sh "$ROOT/spk/scripts/start-stop-status" prestop
test ! -e "$work/var"
rm "$work/target/bin/easytier-pro-dsm"
if sh "$ROOT/spk/scripts/start-stop-status" prestart; then
    echo "prestart accepted a missing daemon" >&2
    exit 1
fi
echo "Lifecycle prechecks passed."
