// Local API client. Every request goes to the same origin as this page, so the
// browser attaches the DSM session cookie automatically. DSM also requires the
// CSRF token it issues with the login session, which lives in the desktop
// shell rather than in a cookie, so it has to be read and forwarded by hand.

import { ApiError } from './errors'
import type {
	AccountPayload,
	AuthPollPayload,
	AuthStartPayload,
	AuthStatus,
	DownloadStatus,
	EnrollmentOptions,
	Envelope,
	LocalSummary,
	LogsPayload,
	NetworksPayload,
	NodesPayload,
	Operation,
	Settings,
	Status,
} from './types'

/**
 * synoToken reads the DSM session token from the surrounding desktop shell.
 *
 * The application window is opened by DSM and shares its origin, so the shell
 * that holds the token is reachable through one of the window references. A
 * page opened directly has no shell and therefore no token; such a request is
 * rejected by DSM, which the interface reports as an expired session.
 */
export function synoToken(): string {
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

interface RequestOptions {
	method?: string
	body?: unknown
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
	const headers: Record<string, string> = { 'X-Easytier-Request': '1' }
	const token = synoToken()
	if (token) {
		headers['X-Syno-Token'] = token
	}
	const init: RequestInit = {
		method: options.method || 'GET',
		headers,
		credentials: 'same-origin',
	}
	if (options.body !== undefined) {
		headers['Content-Type'] = 'application/json'
		init.body = JSON.stringify(options.body)
	}

	let response: Response
	try {
		response = await fetch(path, init)
	} catch {
		throw new ApiError('console_unreachable', '无法连接本机 EasyTier Pro 服务。')
	}
	if (response.status === 401 || response.status === 403) {
		throw new ApiError(response.status === 403 ? 'dsm_auth_forbidden' : 'dsm_auth_required')
	}
	let payload: Envelope
	try {
		payload = await response.json() as Envelope
	} catch {
		throw new ApiError('invalid_console_response', '本机服务返回了无效响应。')
	}
	if (payload.ok === false) {
		throw new ApiError(payload.error || 'unknown_error', payload.message)
	}
	return payload as T
}

/** query builds a query string, dropping absent values. */
function query(params: Record<string, string | number | undefined | null>): string {
	const search = new URLSearchParams()
	for (const [ key, value ] of Object.entries(params)) {
		if (value !== undefined && value !== null && value !== '') {
			search.set(key, String(value))
		}
	}
	const text = search.toString()
	return text ? `?${text}` : ''
}

export const api = {
	status: () => request<Status>('api/status'),
	localSummary: () => request<LocalSummary>('api/local-summary'),
	serviceAction: (action: string) => request<Envelope>('api/service/action', { method: 'POST', body: { action } }),
	applySettings: (settings: Settings) => request<Envelope>('api/settings', { method: 'POST', body: settings }),
	authStatus: () => request<AuthStatus>('api/auth/status'),
	authStart: () => request<AuthStartPayload>('api/auth/device/start', { method: 'POST' }),
	authPoll: () => request<AuthPollPayload>('api/auth/device/poll', { method: 'POST' }),
	// The daemon wraps the Console's own answer under "account", so the envelope
	// is unwrapped here rather than in every caller.
	authMe: async (): Promise<AccountPayload> => {
		const result = await request<{ account?: AccountPayload }>('api/auth/me')
		return result.account || {}
	},
	authLogout: () => request<Envelope>('api/auth/logout', { method: 'POST' }),
	enrollmentOptions: (workspace: string) => request<EnrollmentOptions>(
		`api/workspaces/${encodeURIComponent(workspace)}/enrollment-options`),
	activate: (workspace: string, enrollmentMode: string, enrollmentKeyID: string) => request<Operation>(
		`api/workspaces/${encodeURIComponent(workspace)}/activate`,
		{ method: 'POST', body: { enrollment_mode: enrollmentMode, enrollment_key_id: enrollmentKeyID || '' } },
	),
	connectToken: (bootstrapToken: string, configServer: string) => request<Operation>('api/connect-token', {
		method: 'POST',
		body: { bootstrap_token: bootstrapToken, config_server: configServer || '' },
	}),
	disconnect: () => request<Operation>('api/disconnect', { method: 'POST' }),
	operationStatus: (operationID: string) => request<Operation>(
		`api/connection-change/status${query({ operation_id: operationID })}`),
	networks: () => request<NetworksPayload>('api/networks'),
	networkNodes: (networkID: string) => request<NodesPayload>(
		`api/networks/${encodeURIComponent(networkID)}/nodes`),
	networkJoin: (networkID: string) => request<Operation>(
		`api/networks/${encodeURIComponent(networkID)}/join`, { method: 'POST' }),
	networkLeave: (networkID: string) => request<Operation>(
		`api/networks/${encodeURIComponent(networkID)}/leave`, { method: 'POST' }),
	downloadStart: (version?: string) => request<DownloadStatus>('api/runtime/download',
		{ method: 'POST', body: { version: version || '' } }),
	downloadStatus: () => request<DownloadStatus>('api/runtime/download/status'),
	logs: (lines: number) => request<LogsPayload>(`api/logs${query({ lines })}`),
}

/**
 * consoleWebURL derives the Console web console address from the configured
 * API address, matching the desktop client.
 */
export function consoleWebURL(consoleURL?: string): string {
	let base: URL
	try {
		base = new URL(consoleURL || 'https://api.console.easytier.net')
	} catch {
		return 'https://console.easytier.net/'
	}
	const host = base.hostname === 'api.console.easytier.net' ? 'console.easytier.net' : base.hostname
	return `${base.protocol}//${host}${base.port ? `:${base.port}` : ''}/`
}
