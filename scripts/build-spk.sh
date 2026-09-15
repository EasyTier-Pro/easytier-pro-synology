#!/bin/sh
# Build EasyTier Pro SPK packages for the supported DSM architectures.
#
# Usage: scripts/build-spk.sh [--dsm 6|7|all] [--arch x86_64|armv8|armv7|all] [--version x.y.z-bbbb] [--out dist]
#
# DSM 7 and DSM 6 need separate packages: DSM 7's package installer requires the
# os_min_ver major number to equal the running OS (so a 6.x os_min_ver is rejected
# on DSM 7), and DSM 6 has no conf/resource web-config worker to inject the nginx
# snippet. The default target is all (both DSM majors, every architecture).
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ARCH="all"
DSM="all"
VERSION=""
OUT="$ROOT/dist"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--dsm) DSM="${2:?missing value for --dsm}"; shift 2 ;;
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

# os_min_ver and the package file name differ per DSM major.
dsm_os_min_ver() {
	case "$1" in
		6) printf '6.2-23739' ;;
		7) sed -n 's/^os_min_ver="\(.*\)"$/\1/p' "$ROOT/spk/INFO" ;;
		*) return 1 ;;
	esac
}

dsm_pkg_suffix() {
	case "$1" in
		6) printf -- '-dsm6' ;;
		*) printf '' ;;
	esac
}

# The interface is a bundled build, not a set of source files, so it is produced
# once for every architecture and only then copied into each package.
build_ui() {
	echo "building the interface"
	(
		cd "$ROOT/ui"
		if [ -f package-lock.json ]; then
			npm ci --no-audit --no-fund
		else
			npm install --no-audit --no-fund
		fi
		npm run build:dsm
	)
	[ -f "$ROOT/ui/dist-dsm/index.html" ] || { echo "the interface build produced no index.html" >&2; exit 1; }
}

build_variant() {
	arch="$1"
	dsm="$2"
	env_pair="$(go_env_for_arch "$arch")" || { echo "unsupported arch: $arch" >&2; exit 1; }
	goarch="${env_pair%%:*}"
	goarm="${env_pair#*:}"
	os_min_ver="$(dsm_os_min_ver "$dsm")" || { echo "unsupported dsm: $dsm" >&2; exit 1; }
	pkg_suffix="$(dsm_pkg_suffix "$dsm")"

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	mkdir -p "$work/package/bin" "$work/package/ui" "$work/package/nginx" "$work/package/ui/images"

	echo "building $arch dsm$dsm (GOARCH=$goarch GOARM=${goarm:-default} os_min_ver=$os_min_ver)"
	(
		cd "$ROOT"
		CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" GOARM="$goarm" \
			go build -trimpath -ldflags "-s -w -X main.buildVersion=$VERSION" \
			-o "$work/package/bin/easytier-pro-dsm" ./cmd/easytier-pro-dsm
	)

	# The built interface: the bundle the daemon and nginx serve. DSM's menu
	# entry and nginx both address ui/index.html, which the build produces at the
	# root of dist-dsm/.
	# The build output already contains the DSM application icons, which are
	# taken from the interface's public directory.
	cp -R "$ROOT/ui/dist-dsm/." "$work/package/ui/"
	cp "$ROOT/spk/package/ui/config" "$work/package/ui/config"
	cp "$ROOT/spk/nginx/dsm.easytier-pro.conf" "$work/package/nginx/dsm.easytier-pro.conf"

	(
		cd "$work/package"
		tar czf "$work/package.tgz" .
	)

	sed -e "s/^version=\".*\"$/version=\"$VERSION\"/" \
		-e "s/^arch=\".*\"$/arch=\"$arch\"/" \
		-e "s/^os_min_ver=\".*\"$/os_min_ver=\"$os_min_ver\"/" \
		"$ROOT/spk/INFO" >"$work/INFO"

	# Package metadata that lives next to the SPK, not inside package.tgz.
	cp -R "$ROOT/spk/scripts" "$work/scripts"
	cp -R "$ROOT/spk/conf" "$work/conf"
	# DSM 6 has no conf/resource web-config worker: the nginx snippet is
	# installed by spk/scripts/postinst instead. It also needs postinst and
	# postuninst to run as root so they can write the snippet to
	# /usr/syno/share/nginx/conf.d/, which is owned by root.
	if [ "$dsm" = 6 ]; then
		rm -f "$work/conf/resource"
		cp "$ROOT/spk/conf/privilege.dsm6" "$work/conf/privilege"
	fi
	cp "$ROOT/LICENSE" "$work/LICENSE"
	cp "$ROOT/spk/icons/PACKAGE_ICON.PNG" "$ROOT/spk/icons/PACKAGE_ICON_256.PNG" "$work/"
	chmod 755 "$work/scripts"/*

	mkdir -p "$OUT"
	spk="$OUT/easytier-pro-$arch$pkg_suffix-$VERSION.spk"
	(
		cd "$work"
		tar cf "$spk" INFO package.tgz scripts conf LICENSE \
			PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG
	)
	echo "wrote $spk"
}

build_all_arches() {
	dsm="$1"
	case "$ARCH" in
		all)
			build_variant x86_64 "$dsm"
			build_variant armv8 "$dsm"
			build_variant armv7 "$dsm"
			;;
		*)
			build_variant "$ARCH" "$dsm"
			;;
	esac
}

mkdir -p "$OUT"
build_ui
case "$DSM" in
	all)
		build_all_arches 7
		build_all_arches 6
		;;
	6|7)
		build_all_arches "$DSM"
		;;
	*)
		echo "unsupported dsm: $DSM (expected 6, 7 or all)" >&2
		exit 1
		;;
esac
