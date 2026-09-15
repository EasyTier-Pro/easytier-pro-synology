export interface PlatformConfig {
	/** vite base, e.g. './' or '/app/easytier-pro/' */
	base: string
	/** extra request headers injected by the platform shell */
	extraHeaders(): Record<string, string>
	/** error codes for 401/403 */
	authRequiredCode: string
	authForbiddenCode: string
	/** where the relogin button points (target=_top) */
	reloginURL: string
	/** brand name for user-visible text */
	brandName: string
	/** whether to show the DSM-style TUN privilege notice */
	showTunNotice: boolean
}

import { platform as dsm } from './dsm'
import { platform as fnos } from './fnos'

// The platform is a build-time choice: VITE_PLATFORM selects the shell the
// interface is built for, and every platform-specific text and link below
// derives from this one object.
export const platform: PlatformConfig =
	import.meta.env.VITE_PLATFORM === 'fnos' ? fnos : dsm
