import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { router } from './router'
import { resetShared } from './stores/resources'

// The way to the Console is part of the shell, not of a page: it used to sit in
// the last card of the overview, below the fold, which is the same as not being
// there at all.

const { apiMock } = vi.hoisted(() => ({
	apiMock: {
		status: vi.fn(),
		authStatus: vi.fn(),
		downloadStatus: vi.fn(),
		localSummary: vi.fn(),
		networks: vi.fn(),
	},
}))

vi.mock('@/api/client', async (importOriginal) => {
	const actual = await importOriginal<typeof import('@/api/client')>()
	return { ...actual, api: apiMock }
})

vi.mock('@/naive', () => ({
	notify: vi.fn(),
	confirmDestructive: vi.fn(() => Promise.resolve(true)),
	message: { info: vi.fn(), success: vi.fn(), warning: vi.fn(), error: vi.fn() },
	dialog: { info: vi.fn(() => ({ destroy: vi.fn() })), warning: vi.fn() },
	notification: { info: vi.fn(), success: vi.fn(), warning: vi.fn(), error: vi.fn() },
	isDark: { value: false },
	themeMode: { value: 'system' },
	setThemeMode: vi.fn(),
}))

/** mountShell renders the shell with the routed page stubbed out. */
async function mountShell() {
	await router.push('/overview')
	await router.isReady()
	const wrapper = mount(App, {
		global: { plugins: [ router ], stubs: { RouterView: true } },
	})
	await flushPromises()
	return wrapper
}

function consoleButton(wrapper: Awaited<ReturnType<typeof mountShell>>) {
	return wrapper.findAll('a').find((item) => item.text().includes('打开 Console'))
}

describe('shell', () => {
	beforeEach(() => {
		resetShared()
		for (const fn of Object.values(apiMock)) {
			fn.mockReset()
		}
		apiMock.authStatus.mockResolvedValue({ logged_in: true })
		apiMock.downloadStatus.mockResolvedValue({ state: 'idle' })
	})

	it('offers the Console from the header on every page', async () => {
		apiMock.status.mockResolvedValue({ console_url: 'https://api.console.easytier.net' })
		const wrapper = await mountShell()
		const button = consoleButton(wrapper)
		expect(button).toBeTruthy()
		expect(button!.element.closest('header')).toBeTruthy()
	})

	// The Console routes by fragment, and the device list is where this machine
	// appears, so the button must not land on the Console home page.
	it('points at the Console device list', async () => {
		apiMock.status.mockResolvedValue({ console_url: 'https://api.console.easytier.net' })
		const wrapper = await mountShell()
		expect(consoleButton(wrapper)!.attributes('href')).toBe('https://console.easytier.net/#/devices')
	})

	it('points at a self-hosted Console too', async () => {
		apiMock.status.mockResolvedValue({ console_url: 'https://console.example.com:8443' })
		const wrapper = await mountShell()
		expect(consoleButton(wrapper)!.attributes('href')).toBe('https://console.example.com:8443/#/devices')
	})

	// Before the address is known there is nothing to point at.
	it('offers no Console entry until the address is known', async () => {
		apiMock.status.mockResolvedValue({})
		const wrapper = await mountShell()
		expect(consoleButton(wrapper)).toBeFalsy()
	})

})
