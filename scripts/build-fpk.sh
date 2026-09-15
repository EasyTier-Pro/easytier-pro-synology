#!/bin/sh
# Build EasyTier Pro FPK packages for the supported fnOS architectures.
#
# Usage: scripts/build-fpk.sh [--arch x86_64|arm64|all] [--version x.y.z] [--out dist]
#
# The fnOS package is a gzipped tar with the manifest at its root and the app
# payload (daemon, interface, desktop entry) in app.tgz. When fnpack is
# available it is preferred; otherwise the equivalent manual tar is produced.
set -eu

ROOT="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
ARCH="all"
VERSION=""
OUT="$ROOT/dist"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--arch) ARCH="${2:?missing value for --arch}"; shift 2 ;;
		--version) VERSION="${2:?missing value for --version}"; shift 2 ;;
		--out) OUT="${2:?missing value for --out}"; shift 2 ;;
		-h|--help)
			sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'
			exit 0
			;;
		*)
			echo "unknown argument: $1" >&2
			exit 1
			;;
	esac
done

if [ -z "$VERSION" ]; then
	VERSION="$(sed -n 's/^version=\(.*\)$/\1/p' "$ROOT/fpk/manifest")"
fi
[ -n "$VERSION" ] || { echo "cannot determine the package version" >&2; exit 1; }

# The interface is a bundled build, not a set of source files, so it is
# produced once and only then copied into each package.
build_ui() {
	echo "building the interface"
	(
		cd "$ROOT/ui"
		if [ -f package-lock.json ]; then
			npm ci --no-audit --no-fund
		else
			npm install --no-audit --no-fund
		fi
		npm run build:fnos
	)
	[ -f "$ROOT/ui/dist-fnos/index.html" ] || { echo "the interface build produced no index.html" >&2; exit 1; }
}

build_one() {
	arch="$1"
	case "$arch" in
		x86_64) goarch=amd64; platform=x86 ;;
		arm64) goarch=arm64; platform=arm ;;
		*) echo "unsupported arch: $arch" >&2; exit 1 ;;
	esac

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	mkdir -p "$work/app/bin"

	echo "building $arch (GOARCH=$goarch platform=$platform)"
	(
		cd "$ROOT"
		CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -tags fnos -trimpath \
			-ldflags "-s -w -X main.buildVersion=$VERSION" \
			-o "$work/app/bin/easytier-pro-fnos" ./cmd/easytier-pro-fnos
	)

	# The built interface: the bundle the daemon serves. The desktop entry
	# (ui/config) and the app icons ship beside it.
	cp -R "$ROOT/ui/dist-fnos/." "$work/app/ui/"
	# The interface build already copies the app icons from ui/public, so the
	# source tree must not ship its own copies into the payload.
	cp "$ROOT/fpk/app/ui/config" "$work/app/ui/config"
	mkdir -p "$work/app/ui/images"
	rm -f "$work/app/ui/images/"app_*.png
	cp "$ROOT/fpk/app/ui/images/"* "$work/app/ui/images/"

	(
		cd "$work/app"
		tar czf "$work/app.tgz" .
	)
	sum="$(md5sum "$work/app.tgz" | awk '{print $1}')"

	sed -e "s/^version=.*/version=$VERSION/" \
		-e "s/^platform=.*/platform=$platform/" \
		-e "s/^checksum=.*/checksum=$sum/" "$ROOT/fpk/manifest" >"$work/manifest"

	cp -R "$ROOT/fpk/cmd" "$ROOT/fpk/config" "$ROOT/fpk/wizard" "$work/"
	chmod 755 "$work/cmd/"*
	cp "$ROOT/fpk/ICON.PNG" "$ROOT/fpk/ICON_256.PNG" "$ROOT/LICENSE" "$work/"

	mkdir -p "$OUT"
	fpk="$OUT/easytier-pro-$arch-$VERSION.fpk"
	(
		cd "$work"
		tar czf "$fpk" manifest app.tgz cmd config wizard ICON.PNG ICON_256.PNG LICENSE
	)
	echo "wrote $fpk"
}

mkdir -p "$OUT"
build_ui
case "$ARCH" in
	all)
		build_one x86_64
		build_one arm64
		;;
	x86_64|arm64)
		build_one "$ARCH"
		;;
	*)
		echo "unsupported arch: $ARCH (expected x86_64, arm64 or all)" >&2
		exit 1
		;;
esac
