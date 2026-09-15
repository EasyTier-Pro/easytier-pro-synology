import type { PlatformConfig } from './index'

// fnOS authenticates through its own session cookie; there is no shell token
// to forward, and the whole interface is served under the app prefix.
export const platform: PlatformConfig = {
	base: '/app/easytier-pro/',
	extraHeaders() {
		return {}
	},
	authRequiredCode: 'fnos_auth_required',
	authForbiddenCode: 'fnos_auth_forbidden',
	reloginURL: '/',
	brandName: 'fnOS',
	showTunNotice: false,
}
