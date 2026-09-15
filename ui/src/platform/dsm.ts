import type { PlatformConfig } from './index'

export const platform: PlatformConfig = {
	base: './',
	extraHeaders(): Record<string, string> {
		const token = synoToken()
		return token ? { 'X-Syno-Token': token } : {}
	},
	authRequiredCode: 'dsm_auth_required',
	authForbiddenCode: 'dsm_auth_forbidden',
	reloginURL: '/webman/index.cgi',
	brandName: 'DSM',
	showTunNotice: true,
}

/**
 * synoToken reads the DSM session token from the surrounding desktop shell.
 *
 * The application window is opened by DSM and shares its origin, so the shell
 * that holds the token is reachable through one of the window references. A
 * page opened directly has no shell and therefore no token; such a request is
 * rejected by DSM, which the interface reports as an expired session.
 */
function synoToken(): string {
	const scopes: Array<Window | null> = [
		window,
		window.parent,
		window.top,
		window.opener as Window | null,
	]
	for (const scope of scopes) {
		try {
			const session = (scope as unknown as {
				SYNO?: { SDS?: { Session?: { getSynoToken?: () => string; SynoToken?: string } } }
			})?.SYNO?.SDS?.Session
			if (!session) {
				continue
			}
			const token = typeof session.getSynoToken === 'function'
				? session.getSynoToken()
				: session.SynoToken
			if (token) {
				return token
			}
		} catch {
			// Cross-origin window: not the DSM shell.
		}
	}
	return ''
}
