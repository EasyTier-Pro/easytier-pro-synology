import { beforeEach, describe, expect, it, vi } from 'vitest'
import { showOperationProgress } from './useOperationProgress'

const { apiMock, notifyMock } = vi.hoisted(() => ({
	apiMock: { operationStatus: vi.fn() },
	notifyMock: vi.fn(),
}))

vi.mock('@/api/client', () => ({ api: apiMock, consoleWebURL: (v?: string) => v || '' }))
vi.mock('@/naive', () => ({
	notify: (...args: unknown[]) => notifyMock(...args),
	dialog: { info: vi.fn(() => ({ destroy: vi.fn() })), warning: vi.fn() },
	message: { info: vi.fn(), success: vi.fn(), warning: vi.fn(), error: vi.fn() },
	notification: { info: vi.fn(), success: vi.fn(), warning: vi.fn(), error: vi.fn() },
	isDark: { value: false },
}))

// Run scheduled work immediately so the poll advances without real waiting.
const scope = { after: (_delay: number, callback: () => void) => { callback(); return 0 } }

describe('operation progress', () => {
	beforeEach(() => {
		apiMock.operationStatus.mockReset()
		notifyMock.mockReset()
	})

	// A background change that fails must be said out loud: the dialog closes and
	// the page refreshes, so without this the operator sees the modal vanish and
	// nothing else.
	it('reports a failed operation', async () => {
		apiMock.operationStatus.mockResolvedValue({
			state: 'failed',
			message: 'Console 无法把本机加入该网络。',
		})
		const failed = vi.fn()

		showOperationProgress('正在加入「办公网」', 'op1', { completed: vi.fn(), failed }, scope)
		await new Promise((resolve) => setTimeout(resolve, 0))

		expect(notifyMock).toHaveBeenCalledWith('Console 无法把本机加入该网络。', 'error')
		expect(failed).toHaveBeenCalled()
	})

	it('reports a failed operation that carries no message', async () => {
		apiMock.operationStatus.mockResolvedValue({ state: 'failed' })
		showOperationProgress('正在加入「办公网」', 'op1', { completed: vi.fn() }, scope)
		await new Promise((resolve) => setTimeout(resolve, 0))

		expect(notifyMock).toHaveBeenCalledWith('操作失败，请重试。', 'error')
	})

	it('reports an unreadable operation once the queries run out', async () => {
		apiMock.operationStatus.mockRejectedValue(new Error('连接中断'))
		const failed = vi.fn()
		showOperationProgress('正在加入「办公网」', 'op1', { completed: vi.fn(), failed }, scope)
		await new Promise((resolve) => setTimeout(resolve, 0))

		expect(failed).toHaveBeenCalled()
		expect(notifyMock).toHaveBeenCalledWith(expect.stringContaining('无法查询操作进度'), 'error')
	})

	it('reports nothing on success', async () => {
		apiMock.operationStatus.mockResolvedValue({ state: 'completed' })
		const completed = vi.fn()
		showOperationProgress('正在加入「办公网」', 'op1', { completed }, scope)
		await new Promise((resolve) => setTimeout(resolve, 0))

		expect(completed).toHaveBeenCalled()
		expect(notifyMock).not.toHaveBeenCalled()
	})
})
