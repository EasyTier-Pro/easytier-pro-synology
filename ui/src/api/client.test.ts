import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, consoleWebURL } from './client'

// These go through the real request path against a stubbed fetch, so the shapes
// asserted here are the ones the daemon actually sends. A double that returned
// the shape the interface wished for would hide exactly this class of bug.

const envelope = (payload: unknown) => ({ ok: true, ...(payload as object) })

function respondWith(payload: unknown) {
	const fetchMock = vi.fn(() => Promise.resolve({
		status: 200,
		json: () => Promise.resolve(envelope(payload)),
	} as unknown as Response))
	vi.stubGlobal('fetch', fetchMock)
	return fetchMock
}

describe('api client', () => {
	afterEach(() => {
		vi.unstubAllGlobals()
	})

	// internal/httpserver answers `{"ok":true,"account":{...}}`; the page needs
	// the account itself. Reading `.tenants` off the envelope finds nothing, which
	// makes the workspace flow report that the account has no workspace at all.
	it('unwraps the account the daemon returns', async () => {
		respondWith({ account: { tenants: [ { id: 't1', name: 'Acme' } ] } })
		const account = await api.authMe()
		expect(account.tenants).toHaveLength(1)
		expect(account.tenants?.[0].id).toBe('t1')
	})

	it('tolerates a missing account', async () => {
		respondWith({})
		expect((await api.authMe()).tenants).toBeUndefined()
	})

	it('unwraps the enrollment options the daemon returns', async () => {
		respondWith({ local_connection: true, reusable_keys: [ { id: 'k1' } ] })
		const options = await api.enrollmentOptions('t1')
		expect(options.local_connection).toBe(true)
		expect(options.reusable_keys).toHaveLength(1)
	})

	it('reads the operation state the daemon reports', async () => {
		respondWith({ operation_id: 'op1', state: 'completed', needs_download: true })
		const operation = await api.operationStatus('op1')
		expect(operation.state).toBe('completed')
		expect(operation.needs_download).toBe(true)
	})

	it('accepts an operation as soon as it is queued', async () => {
		respondWith({ operation_id: 'op1', state: 'queued' })
		expect((await api.networkJoin('n1')).operation_id).toBe('op1')
	})

	it('turns a transport failure into a reported error', async () => {
		vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new Error('offline'))))
		await expect(api.status()).rejects.toMatchObject({ code: 'console_unreachable' })
	})

	it('turns an expired session into the code the relogin path looks for', async () => {
		vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ status: 401 } as Response)))
		await expect(api.status()).rejects.toMatchObject({ code: 'dsm_auth_required' })
	})

	it('reports the daemon error code and message', async () => {
		respondWith({ ok: false, error: 'network_join_failed', message: 'Console 拒绝了加入请求。' })
		await expect(api.networkJoin('n1')).rejects.toMatchObject({
			code: 'network_join_failed',
			message: 'Console 拒绝了加入请求。',
		})
	})
})

describe('console web address', () => {
	it('rewrites the API host to the web host', () => {
		expect(consoleWebURL('https://api.console.easytier.net')).toBe('https://console.easytier.net/')
	})

	it('keeps an explicit port', () => {
		expect(consoleWebURL('http://console.example.com:8080'))
			.toBe('http://console.example.com:8080/')
	})

	it('falls back to the public console for an unusable address', () => {
		expect(consoleWebURL('not a url')).toBe('https://console.easytier.net/')
	})
})
