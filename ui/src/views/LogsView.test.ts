import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import LogsView from './LogsView.vue'
import { logLineCount, resetShared } from '@/stores/resources'

const { apiMock } = vi.hoisted(() => ({
	apiMock: {
		status: vi.fn(),
		localSummary: vi.fn(),
		logs: vi.fn(),
		serviceAction: vi.fn(),
	},
}))

vi.mock('@/api/client', () => ({ api: apiMock, consoleWebURL: (value?: string) => value || '' }))
vi.mock('@/naive', () => ({
	notify: vi.fn(),
	confirmDestructive: vi.fn(() => Promise.resolve(true)),
	message: { info: vi.fn(), success: vi.fn(), warning: vi.fn(), error: vi.fn() },
	dialog: { info: vi.fn(() => ({ destroy: vi.fn() })), warning: vi.fn() },
	notification: { info: vi.fn(), success: vi.fn(), warning: vi.fn(), error: vi.fn() },
	isDark: { value: false },
}))

describe('logs', () => {
	beforeEach(() => {
		resetShared()
		logLineCount.value = 200
		for (const fn of Object.values(apiMock)) {
			fn.mockReset()
		}
		apiMock.status.mockResolvedValue({ running: true, machine_id: 'm1', core_version: 'v2.6.4' })
		apiMock.localSummary.mockResolvedValue({ interfaces: [ 'tun0' ] })
		apiMock.logs.mockResolvedValue({ lines: 200, logs: 'line' })
	})

	afterEach(() => {
		vi.restoreAllMocks()
	})

	it('reads the log tail for the selected line count', async () => {
		const wrapper = mount(LogsView)
		await flushPromises()
		expect(apiMock.logs).toHaveBeenCalledWith(200)

		// The choice is a preference, so it is read again when asked for more.
		logLineCount.value = 500
		await wrapper.findAll('button').find((item) => item.text().includes('刷新'))!.trigger('click')
		await flushPromises()
		expect(apiMock.logs).toHaveBeenLastCalledWith(500)
	})

	// Leaving the page and coming back must keep the operator's choice. This was
	// kept at module level in the previous interface and became per-instance when
	// the page moved to a component.
	it('keeps the chosen line count across navigation', async () => {
		const first = mount(LogsView)
		await flushPromises()
		logLineCount.value = 500
		first.unmount()

		const second = mount(LogsView)
		await flushPromises()
		expect(apiMock.logs).toHaveBeenLastCalledWith(500)
		second.unmount()
	})
})
