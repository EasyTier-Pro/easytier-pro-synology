#!/bin/sh
# Verify that the built SPK packages are complete and internally consistent.
#
# Usage: scripts/check-spk.sh dist/easytier-pro-*.spk
set -eu

fail() {
	echo "check-spk: $*" >&2
	exit 1
}

usage() {
	sed -n '2,3p' "$0" | sed 's/^# \{0,1\}//'
	exit 1
}

[ "$#" -ge 1 ] || usage
command -v jq >/dev/null 2>&1 || fail "jq is required"

required_info_keys="package version os_min_ver arch maintainer maintainer_url distributor distributor_url support_url displayname description description_chs dsmuidir dsmappname precheckstartstop extractsize"
required_package_files="bin/easytier-pro-dsm ui/index.html ui/config nginx/dsm.easytier-pro.conf"

for spk in "$@"; do
	[ -f "$spk" ] || fail "missing file: $spk"
	members="$(tar tf "$spk")"
	for member in INFO package.tgz scripts/start-stop-status conf/privilege PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG; do
		printf '%s\n' "$members" | grep -qx "$member" || fail "$spk: missing member $member"
	done

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	tar xf "$spk" -C "$work" INFO package.tgz conf scripts

	for key in $required_info_keys; do
		grep -q "^$key=\"[^\"]*\"$" "$work/INFO" || fail "$spk: INFO has no usable $key"
	done

	os_min_ver="$(sed -n 's/^os_min_ver="\(.*\)"$/\1/p' "$work/INFO")"
	case "$os_min_ver" in
		7.*) ;;
		6.2-*) ;;
		*) fail "$spk: os_min_ver $os_min_ver is neither a DSM 7 nor a DSM 6.2+ floor" ;;
	esac

	arch="$(sed -n 's/^arch="\(.*\)"$/\1/p' "$work/INFO")"
	case "$arch" in
		x86_64|armv8|armv7) ;;
		*) fail "$spk: unsupported arch $arch" ;;
	esac
	# The file name carries the arch, and optionally a -dsm6 suffix after it.
	case "$spk" in
		*"-$arch-"*|*"-$arch-dsm6-"*) ;;
		*) fail "$spk: file name does not match INFO arch $arch" ;;
	esac
	# os_min_ver major and the -dsm6 suffix must agree.
	case "$os_min_ver" in
		6.*)
			case "$spk" in
				*"-dsm6-"*) ;;
				*) fail "$spk: DSM 6 package must carry the -dsm6 suffix" ;;
			esac
			;;
		7.*)
			case "$spk" in
				*"-dsm6-"*) fail "$spk: DSM 7 package must not carry the -dsm6 suffix" ;;
			esac
			;;
	esac

	jq -e . "$work/conf/privilege" >/dev/null || fail "$spk: conf/privilege is not valid JSON"
	jq -e '."defaults"."run-as" == "package"' "$work/conf/privilege" >/dev/null \
		|| fail "$spk: conf/privilege defaults must run as package (DSM rejects root defaults)"
	# DSM 7 rejects ctrl-script for unsigned packages, but DSM 6 needs it to run
	# postinst/postuninst/preuninst/preupgrade/start/stop as root.
	case "$os_min_ver" in
		7.*)
			jq -e 'has("ctrl-script") | not' "$work/conf/privilege" >/dev/null \
				|| fail "$spk: conf/privilege must not declare ctrl-script (DSM rejects it for unsigned packages)"
			;;
		6.*)
			jq -e '."ctrl-script"' "$work/conf/privilege" >/dev/null \
				|| fail "$spk: DSM 6 package must declare ctrl-script for root lifecycle scripts"
			;;
	esac
	jq -e 'has("executable") | not' "$work/conf/privilege" >/dev/null \
		|| fail "$spk: conf/privilege must not declare executable (DSM rejects it for unsigned packages)"
	jq -e '[."tool"[]? | select(has("capabilities"))] | length == 0' "$work/conf/privilege" >/dev/null \
		|| fail "$spk: conf/privilege must not request tool capabilities (DSM rejects it for unsigned packages)"
	# DSM 7 needs the web-config resource worker to inject the nginx snippet; DSM 6
	# packages omit conf/resource entirely and inject via postinst instead.
	case "$os_min_ver" in
		7.*)
			jq -e . "$work/conf/resource" >/dev/null || fail "$spk: DSM 7 package is missing conf/resource"
			jq -e '."web-config"."nginx-static-config"' "$work/conf/resource" >/dev/null \
				|| fail "$spk: conf/resource is missing the nginx-static-config worker"
			;;
		6.*)
			[ ! -e "$work/conf/resource" ] || fail "$spk: DSM 6 package must not ship conf/resource"
			;;
	esac

	[ -x "$work/scripts/start-stop-status" ] || fail "$spk: scripts/start-stop-status is not executable"
	for script in preinst postinst preupgrade postupgrade preuninst postuninst; do
		[ -x "$work/scripts/$script" ] || fail "$spk: scripts/$script is not executable"
	done

	package_members="$(tar tzf "$work/package.tgz")"
	# The interface ships to users; its test suite must not. The suite is
	# TypeScript under src/, which the leak check below rejects wholesale, so this
	# only has to catch a test file that ever reaches the build output.
	if printf '%s\n' "$package_members" | grep -qE '(^|/)[^/]*\.(test|spec)\.[jt]sx?$'; then
		fail "$spk: package.tgz ships test files: $(printf '%s\n' "$package_members" | grep -E '(^|/)[^/]*\.(test|spec)\.[jt]sx?$' | tr '\n' ' ')"
	fi
	for member in $required_package_files; do
		printf '%s\n' "$package_members" | grep -qx "./$member" \
			|| printf '%s\n' "$package_members" | grep -qx "$member" \
			|| fail "$spk: package.tgz is missing $member"
	done
	# The interface is a built bundle, so the assets carry hashed names and only
	# their presence and kind can be checked.
	printf '%s\n' "$package_members" | grep -qE '(^|/|\./)ui/assets/[^/]+\.js$' \
		|| fail "$spk: package.tgz has no bundled interface script"
	printf '%s\n' "$package_members" | grep -qE '(^|/|\./)ui/assets/[^/]+\.css$' \
		|| fail "$spk: package.tgz has no bundled interface stylesheet"

	# Nothing from the build environment may reach the appliance: neither the
	# interface sources and their tests, nor the package manager's tree, nor the
	# source maps that would only enlarge the package.
	for pattern in 'ui/src/' 'ui/node_modules/' 'ui/package.json' 'ui/package-lock.json' \
		'ui/tsconfig.json' 'ui/vite.config.ts' 'ui/embed.go' 'ui/.*\.test\.' '\.map$'; do
		if printf '%s\n' "$package_members" | grep -qE "$pattern"; then
			fail "$spk: package.tgz leaks $pattern"
		fi
	done

	tar xzf "$work/package.tgz" -C "$work" ./ui/config 2>/dev/null || tar xzf "$work/package.tgz" -C "$work" ui/config
	ui_config="$work/ui/config"
	[ -f "$ui_config" ] || fail "$spk: package.tgz is missing ui/config"
	jq -e '.[".url"] | type == "object"' "$ui_config" >/dev/null \
		|| fail "$spk: ui/config is not a valid DSM url entry"
	jq -e '.[".url"]."SYNO.SDS.EasyTierPro"."url" == "3rdparty/easytier-pro/index.html"' "$ui_config" >/dev/null \
		|| fail "$spk: ui/config does not point at the bundled interface"

	echo "ok: $spk ($arch, os_min_ver $os_min_ver)"
	rm -rf "$work"
	trap - EXIT
done
