#!/bin/sh
# Build EasyTier Pro SPK packages for the supported DSM architectures.
#
# Usage: scripts/build-spk.sh [--arch x86_64|armv8|armv7|all] [--version x.y.z-bbbb] [--out dist]
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ARCH="all"
VERSION=""
OUT="$ROOT/dist"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--arch) ARCH="${2:?missing value for --arch}"; shift 2 ;;
		--version) VERSION="${2:?missing value for --version}"; shift 2 ;;
		--out) OUT="${2:?missing value for --out}"; shift 2 ;;
		-h|--help)
			sed -n '2,4p' "$0" | sed 's/^# \{0,1\}//'
			exit 0
			;;
		*)
			echo "unknown argument: $1" >&2
			exit 1
			;;
	esac
done

if [ -z "$VERSION" ]; then
	VERSION="$(sed -n 's/^version="\(.*\)"$/\1/p' "$ROOT/spk/INFO")"
fi
[ -n "$VERSION" ] || { echo "cannot determine the package version" >&2; exit 1; }

go_env_for_arch() {
	case "$1" in
		x86_64) printf 'amd64:' ;;
		armv8) printf 'arm64:' ;;
		armv7) printf 'arm:7' ;;
		*) return 1 ;;
	esac
}

build_variant() {
	arch="$1"
	env_pair="$(go_env_for_arch "$arch")" || { echo "unsupported arch: $arch" >&2; exit 1; }
	goarch="${env_pair%%:*}"
	goarm="${env_pair#*:}"

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	mkdir -p "$work/package/bin" "$work/package/ui" "$work/package/nginx" "$work/package/ui/images"

	echo "building $arch (GOARCH=$goarch GOARM=${goarm:-default})"
	(
		cd "$ROOT"
		CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" GOARM="$goarm" \
			go build -trimpath -ldflags "-s -w -X main.buildVersion=$VERSION" \
			-o "$work/package/bin/easytier-pro-dsm" ./cmd/easytier-pro-dsm
	)

	# Static interface, without the Go source file that embeds it and without
	# the browser test harness, which is development-only.
	(
		cd "$ROOT/ui"
		find . -type f ! -name 'embed.go' ! -name '*.test.mjs' ! -name 'testdom.mjs' -print \
			| while IFS= read -r file; do
			target="$work/package/ui/${file#./}"
			mkdir -p "$(dirname "$target")"
			cp "$file" "$target"
		done
	)
	cp "$ROOT/spk/package/ui/config" "$work/package/ui/config"
	cp "$ROOT/spk/nginx/easytier-pro.conf" "$work/package/nginx/easytier-pro.conf"

	(
		cd "$work/package"
		tar czf "$work/package.tgz" .
	)

	sed "s/^version=\".*\"$/version=\"$VERSION\"/; s/^arch=\".*\"$/arch=\"$arch\"/" \
		"$ROOT/spk/INFO" >"$work/INFO"

	# Package metadata that lives next to the SPK, not inside package.tgz.
	cp -R "$ROOT/spk/scripts" "$work/scripts"
	cp -R "$ROOT/spk/conf" "$work/conf"
	cp "$ROOT/LICENSE" "$work/LICENSE"
	cp "$ROOT/spk/icons/PACKAGE_ICON.PNG" "$ROOT/spk/icons/PACKAGE_ICON_256.PNG" "$work/"
	chmod 755 "$work/scripts"/*

	mkdir -p "$OUT"
	spk="$OUT/easytier-pro-$arch-$VERSION.spk"
	(
		cd "$work"
		tar cf "$spk" INFO package.tgz scripts conf LICENSE \
			PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG
	)
	echo "wrote $spk"
}

mkdir -p "$OUT"
case "$ARCH" in
	all)
		build_variant x86_64
		build_variant armv8
		build_variant armv7
		;;
	*)
		build_variant "$ARCH"
		;;
esac
