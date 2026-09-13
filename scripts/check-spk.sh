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

# version_ge compares "7.0-40000" style versions.
version_ge() {
	awk -v left="$1" -v right="$2" 'BEGIN {
		split(left, l, "-"); split(right, r, "-")
		split(l[1], lv, "."); split(r[1], rv, ".")
		for (i = 1; i <= 3; i++) {
			lp = lv[i] + 0; rp = rv[i] + 0
			if (lp > rp) exit 0
			if (lp < rp) exit 1
		}
		lb = l[2] + 0; rb = r[2] + 0
		exit (lb >= rb) ? 0 : 1
	}'
}

required_info_keys="package version os_min_ver arch maintainer maintainer_url distributor distributor_url support_url displayname description description_chs dsmuidir dsmappname precheckstartstop extractsize"
required_members="INFO package.tgz scripts/start-stop-status conf/privilege conf/resource PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG"
required_package_files="bin/easytier-pro-dsm ui/index.html ui/config ui/app.js ui/styles.css ui/lib/api.js ui/lib/dom.js ui/pages/overview.js ui/pages/networks.js ui/pages/settings.js ui/pages/logs.js nginx/easytier-pro.conf"

for spk in "$@"; do
	[ -f "$spk" ] || fail "missing file: $spk"
	members="$(tar tf "$spk")"
	for member in $required_members; do
		printf '%s\n' "$members" | grep -qx "$member" || fail "$spk: missing member $member"
	done

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	tar xf "$spk" -C "$work" INFO package.tgz conf scripts

	for key in $required_info_keys; do
		grep -q "^$key=\"[^\"]*\"$" "$work/INFO" || fail "$spk: INFO has no usable $key"
	done

	os_min_ver="$(sed -n 's/^os_min_ver="\(.*\)"$/\1/p' "$work/INFO")"
	version_ge "$os_min_ver" "7.0-40000" || fail "$spk: os_min_ver $os_min_ver is below 7.0-40000"

	arch="$(sed -n 's/^arch="\(.*\)"$/\1/p' "$work/INFO")"
	case "$arch" in
		x86_64|armv8|armv7) ;;
		*) fail "$spk: unsupported arch $arch" ;;
	esac
	case "$spk" in
		*"-$arch-"*) ;;
		*) fail "$spk: file name does not match INFO arch $arch" ;;
	esac

	jq -e . "$work/conf/privilege" >/dev/null || fail "$spk: conf/privilege is not valid JSON"
	jq -e . "$work/conf/resource" >/dev/null || fail "$spk: conf/resource is not valid JSON"
	jq -e '."defaults"."run-as" == "root"' "$work/conf/privilege" >/dev/null \
		|| fail "$spk: conf/privilege does not run as root"
	jq -e '."web-config"."nginx-static-config"' "$work/conf/resource" >/dev/null \
		|| fail "$spk: conf/resource is missing the nginx-static-config worker"

	[ -x "$work/scripts/start-stop-status" ] || fail "$spk: scripts/start-stop-status is not executable"
	for script in preinst postinst preupgrade postupgrade preuninst postuninst; do
		[ -x "$work/scripts/$script" ] || fail "$spk: scripts/$script is not executable"
	done

	package_members="$(tar tzf "$work/package.tgz")"
	for member in $required_package_files; do
		printf '%s\n' "$package_members" | grep -qx "./$member" \
			|| printf '%s\n' "$package_members" | grep -qx "$member" \
			|| fail "$spk: package.tgz is missing $member"
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
