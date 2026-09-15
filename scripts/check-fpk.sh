#!/bin/sh
# Verify that the built FPK packages are complete and internally consistent.
#
# Usage: scripts/check-fpk.sh dist/easytier-pro-*.fpk
set -eu

fail() {
	echo "check-fpk: $*" >&2
	exit 1
}

usage() {
	sed -n '2,3p' "$0" | sed 's/^# \{0,1\}//'
	exit 1
}

[ "$#" -ge 1 ] || usage
command -v jq >/dev/null 2>&1 || fail "jq is required"

required_manifest_keys="appname version display_name desc source platform maintainer maintainer_url distributor distributor_url os_min_version ctl_stop desktop_uidir desktop_applaunchname checksum"
required_app_files="bin/easytier-pro-fnos ui/index.html ui/config ui/images/icon_64.png"
required_cmd_scripts="install_init install_callback main upgrade_init upgrade_callback uninstall_init uninstall_callback"

for fpk in "$@"; do
	[ -f "$fpk" ] || fail "missing file: $fpk"

	# 1. Every top-level member is present.
	members="$(tar tzf "$fpk")"
	for member in manifest app.tgz config/privilege config/resource ICON.PNG ICON_256.PNG LICENSE; do
		printf '%s\n' "$members" | grep -qx "$member" || fail "$fpk: missing member $member"
	done
	for script in $required_cmd_scripts; do
		printf '%s\n' "$members" | grep -qx "cmd/$script" || fail "$fpk: missing member cmd/$script"
	done
	printf '%s\n' "$members" | grep -q '^wizard/' || fail "$fpk: missing member wizard/"

	work="$(mktemp -d)"
	trap 'rm -rf "$work"' EXIT
	tar xzf "$fpk" -C "$work" manifest app.tgz cmd config

	# 2. The manifest carries every required key with a usable value.
	for key in $required_manifest_keys; do
		grep -q "^$key=..*$" "$work/manifest" || fail "$fpk: manifest has no usable $key"
	done
	sum="$(sed -n 's/^checksum=\(.*\)$/\1/p' "$work/manifest")"
	printf '%s' "$sum" | grep -qE '^[0-9a-f]{32}$' || fail "$fpk: manifest checksum is not an md5 hex digest"

	# 3. The platform is supported and agrees with the file name.
	platform="$(sed -n 's/^platform=\(.*\)$/\1/p' "$work/manifest")"
	case "$platform" in
		x86) arch=x86_64 ;;
		arm) arch=arm64 ;;
		*) fail "$fpk: unsupported platform $platform" ;;
	esac
	case "$fpk" in
		*"-$arch-"*) ;;
		*) fail "$fpk: file name does not match manifest platform $platform" ;;
	esac

	# 4. config/privilege is valid JSON and runs as root.
	jq -e . "$work/config/privilege" >/dev/null || fail "$fpk: config/privilege is not valid JSON"
	jq -e '."defaults"."run-as" == "root"' "$work/config/privilege" >/dev/null \
		|| fail "$fpk: config/privilege defaults must run as root"

	# 6. Every lifecycle script is executable.
	for script in $required_cmd_scripts; do
		[ -x "$work/cmd/$script" ] || fail "$fpk: cmd/$script is not executable"
	done

	app_members="$(tar tzf "$work/app.tgz")"
	# 7. app.tgz ships the daemon, the interface entry and the app icons.
	for member in $required_app_files; do
		printf '%s\n' "$app_members" | grep -qx "./$member" \
			|| printf '%s\n' "$app_members" | grep -qx "$member" \
			|| fail "$fpk: app.tgz is missing $member"
	done
	# The interface is a built bundle, so the assets carry hashed names and only
	# their presence and kind can be checked.
	printf '%s\n' "$app_members" | grep -qE '(^|/|\./)ui/assets/[^/]+\.js$' \
		|| fail "$fpk: app.tgz has no bundled interface script"
	printf '%s\n' "$app_members" | grep -qE '(^|/|\./)ui/assets/[^/]+\.css$' \
		|| fail "$fpk: app.tgz has no bundled interface stylesheet"

	# 5. The desktop entry points at the gateway prefix and socket.
	tar xzf "$work/app.tgz" -C "$work" ./ui/config 2>/dev/null || tar xzf "$work/app.tgz" -C "$work" ui/config
	ui_config="$work/ui/config"
	[ -f "$ui_config" ] || fail "$fpk: app.tgz is missing ui/config"
	jq -e '.[".url"]."easytier-pro.main".gatewayPrefix == "/app/easytier-pro"' "$ui_config" >/dev/null \
		|| fail "$fpk: ui/config gatewayPrefix is not /app/easytier-pro"
	jq -e '.[".url"]."easytier-pro.main".gatewaySocket == "app.sock"' "$ui_config" >/dev/null \
		|| fail "$fpk: ui/config gatewaySocket is not app.sock"

	# 8. Nothing from the build environment or the other platform's packaging
	# may reach the appliance: neither the interface sources and their tests,
	# nor the package manager's tree, nor the source maps, nor the other
	# platform's names.
	for pattern in 'ui/src/' 'ui/node_modules/' 'ui/package.json' 'ui/package-lock.json' \
		'ui/tsconfig' 'ui/vite\.config\.' 'ui/embed.*\.go' '\.test\.' '\.map$' \
		'ui/images/app_[^/]*\.png$' \
		'easytier-pro-dsm' 'dsm' 'Synology' 'synology' 'SYNOPKG' '群晖' 'X-Syno'; do
		if printf '%s\n' "$app_members" | grep -qiE "$pattern"; then
			fail "$fpk: app.tgz leaks $pattern"
		fi
	done

	# 9. The manifest checksum matches the shipped app.tgz.
	actual="$(md5sum "$work/app.tgz" | awk '{print $1}')"
	[ "$sum" = "$actual" ] || fail "$fpk: manifest checksum $sum does not match app.tgz md5 $actual"

	echo "ok: $fpk (platform $platform)"
	rm -rf "$work"
	trap - EXIT
done
