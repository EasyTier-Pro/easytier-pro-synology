import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import OverviewView from './OverviewView.vue'
import { resetShared } from '@/stores/resources'
import type { Status } from '@/api/types'

// The API and the notification surface are the two things this view talks to, so
// both are replaced here: the assertions are about what the page shows and which
// regions it reloads, not about the transport.

// vi.mock is hoisted above the module body, so the doubles it closes over have
// to be created by vi.hoisted as well.
const { apiMock, notifyMock } = vi.hoisted(() => ({
	apiMock: {
		status: vi.fn(),
		authStatus: vi.fn(),
		downloadStatus: vi.fn(),
		localSummary: vi.fn(),
		networks: vi.fn(),
		downloadStart: vi.fn(),
		serviceAction: vi.fn(),
		networkJoin: vi.fn(),
		networkLeave: vi.fn(),
		authLogout: vi.fn(),
		disconnect: vi.fn(),
		authMe: vi.fn(),
		enrollmentOptions: vi.fn(),
		activate: vi.fn(),
		connectToken: vi.fn(),
		operationStatus: vi.fn(),
	},
	notifyMock: vi.fn(),
}))

vi.mock('@/api/client', () => ({
	api: apiMock,
	consoleWebURL: (value?: string) => value || 'https://console.easytier.net/',
}))

vi.mock('@/naive', () => ({
	notify: (...args: unknown[]) => notifyMock(...args),
	confirmDestructive: vi.fn(() => Promise.resolve(true)),
	message: { info: vi.fn(), success: vi.fn(), warning: vi.fn(), error: vi.fn() },
	dialog: { info: vi.fn(() => ({ destroy: vi.fn() })), warning: vi.fn() },
	notification: { info: vi.fn(), success: vi.fn(), warning: vi.fn(), error: vi.fn() },
	isDark: { value: false },
}))

const baseStatus: Status = {
	core_installed: true,
	cli_installed: true,
	running: true,
	token_present: true,
	tun_capable: true,
	bind_capable: true,
	mode_synced: true,
	console_url: 'https://api.console.easytier.net',
	core_version: 'v2.6.4',
	cli_version: 'v2.6.4',
}

function router() {
	return createRouter({
		history: createMemoryHistory(),
		routes: [
			{ path: '/overview', name: 'overview', component: { template: '<div />' } },
			{ path: '/settings', name: 'settings', component: { template: '<div />' } },
			{ path: '/logs', name: 'logs', component: { template: '<div />' } },
		],
	})
}

interface MountOptions {
	status?: Partial<Status>
	loggedIn?: boolean
	networks?: unknown
	networksError?: Error
	summary?: unknown
	summaryError?: Error
	download?: unknown
	/** Delays the download answer, so the two initial requests settle apart. */
	downloadDelayMs?: number
}

async function mountView(options: MountOptions = {}) {
	const { status = {}, loggedIn = true } = options
	apiMock.status.mockResolvedValue({ ...baseStatus, ...status })
	apiMock.authStatus.mockResolvedValue({ logged_in: loggedIn })
	const downloadAnswer = options.download ?? { state: 'idle', phase: '', percent: 0 }
	if (options.downloadDelayMs) {
		// The screen is chosen from `status`; a slower download answer reproduces
		// the order a real install produces when the page is opened mid-install.
		apiMock.downloadStatus.mockImplementation(() => new Promise((resolve) => {
			setTimeout(() => resolve(downloadAnswer), options.downloadDelayMs)
		}))
	} else {
		apiMock.downloadStatus.mockResolvedValue(downloadAnswer)
	}
	if (options.summaryError) {
		apiMock.localSummary.mockRejectedValue(options.summaryError)
	} else {
		apiMock.localSummary.mockResolvedValue(options.summary ?? {
			node: { ipv4_addr: '10.0.0.2' }, peers: [], interfaces: [ 'tun0' ],
		})
	}
	if (options.networksError) {
		apiMock.networks.mockRejectedValue(options.networksError)
	} else {
		apiMock.networks.mockResolvedValue(options.networks ?? {
			machine_id: 'm1', machine: { networks: [] }, networks: [], enrolled: true,
		})
	}

	const instance = mount(OverviewView, {
		global: { plugins: [ router() ] },
	})
	await flushPromises()
	return instance
}

describe('overview', () => {
	beforeEach(() => {
		resetShared()
		for (const fn of Object.values(apiMock)) {
			fn.mockReset()
		}
		notifyMock.mockReset()
	})

	afterEach(() => {
		vi.restoreAllMocks()
	})

	it('first use offers setup instead of account actions', async () => {
		const wrapper = await mountView({
			loggedIn: false,
			status: { token_present: false, core_installed: false, cli_installed: false, running: false },
		})
		const text = wrapper.text()
		expect(text).toContain('开始使用')
		expect(text).toContain('首次使用')
		// 还没有账号也没有令牌时，不应该出现只有已连接设备才有意义的账号操作。
		expect(text).not.toContain('断开本机')
	})

	it('runtime setup offers a way back to the start', async () => {
		const wrapper = await mountView({ status: { core_installed: false, cli_installed: false, running: false } })
		expect(wrapper.text()).toContain('安装 EasyTier 运行时')
		// 运行时还没装好时，用户仍然必须能换账号，否则就被困住了。
		expect(wrapper.text()).toContain('断开本机')
	})

	it('runtime setup keeps its install action', async () => {
		const wrapper = await mountView({ status: { core_installed: false, cli_installed: false, running: false } })
		apiMock.downloadStart.mockResolvedValue({})
		const button = wrapper.findAll('button').find((item) => item.text().includes('安装并继续'))
		expect(button).toBeTruthy()
		await button!.trigger('click')
		await flushPromises()
		expect(apiMock.downloadStart).toHaveBeenCalled()
	})

	it('a failed install offers a retry instead of a first install', async () => {
		const wrapper = await mountView({
			status: { core_installed: false, cli_installed: false, running: false },
			download: { state: 'failed', phase: 'install', message: '新运行时未能启动。' },
		})
		expect(wrapper.text()).toContain('重试安装')
		expect(wrapper.text()).toContain('新运行时未能启动。')
	})

	it('workspace setup offers logout', async () => {
		const wrapper = await mountView({ status: { token_present: false } })
		expect(wrapper.text()).toContain('完成本机设置')
		expect(wrapper.text()).toContain('退出 Console 登录')
	})

	it('no-TUN notice describes reachability, not isolation', async () => {
		const wrapper = await mountView({ status: { tun_capable: false, mode_synced: true } })
		const text = wrapper.text()
		expect(text).toContain('无 TUN 模式')
		// 关键点：本机仍然在网内，别人能访问它；只有本机主动访问别人受影响。
		expect(text).toContain('其它节点可以主动访问本机')
		expect(text).toContain('无法主动访问网络里的其它节点')
	})

	it('unsynced bind capability asks for a Console login', async () => {
		const wrapper = await mountView({ status: { bind_capable: false, mode_synced: false } })
		expect(wrapper.text()).toContain('请先登录 EasyTier Console')
	})

	it('the capability name never reaches the user', async () => {
		const wrapper = await mountView({ status: { tun_capable: false, bind_capable: false, mode_synced: false } })
		const text = wrapper.text()
		// 权限名是套件内部的实现细节，用户既看不懂也无法处理。
		expect(text).not.toContain('CAP_NET_ADMIN')
		expect(text).not.toContain('CAP_NET_RAW')
		expect(text).not.toContain('cap_net_admin')
		expect(text).not.toContain('cap_net_raw')
	})

	it('synced bind capability shows no notice at all', async () => {
		const wrapper = await mountView({ status: { bind_capable: true, mode_synced: true } })
		expect(wrapper.text()).not.toContain('尚未完成网络设置同步')
	})

	it('no warning once the capability is present', async () => {
		const wrapper = await mountView({ status: { tun_capable: true } })
		expect(wrapper.text()).not.toContain('无 TUN 模式')
	})
})

describe('overview region updates', () => {
	beforeEach(() => {
		resetShared()
		for (const fn of Object.values(apiMock)) {
			fn.mockReset()
		}
		notifyMock.mockReset()
	})

	// The defect this guards: an action used to re-render the whole page, which
	// blanked the content area, dropped the scroll position and collapsed every
	// expanded section. Each region now updates on its own, so an element outside
	// the region that changed must survive the action untouched.
	it('an action reloads only its own region and leaves the rest of the page in place', async () => {
		apiMock.networkJoin.mockResolvedValue({ operation_id: 'op1', state: 'queued' })
		// The operation completes on the first poll, so the join finishes.
		apiMock.operationStatus.mockResolvedValue({ state: 'completed', needs_download: false })

		const wrapper = await mountView({ networks: {
			machine_id: 'm1',
			machine: { networks: [] },
			networks: [ { id: 'n1', name: '办公网', ipv4_cidr: '10.10.0.0/24' } ],
			enrolled: true,
		} })
		const statusCard = wrapper.findAll('.n-card')[0].element

		const joinButton = wrapper.findAll('button').find((item) => item.text().includes('加入'))
		expect(joinButton).toBeTruthy()
		await joinButton!.trigger('click')
		await flushPromises()
		await new Promise((resolve) => setTimeout(resolve, 1400))
		await flushPromises()

		// The status region was never asked to reload, and its DOM node is the
		// same element rather than a replacement.
		expect(apiMock.networkJoin).toHaveBeenCalledWith('n1')
		expect(wrapper.findAll('.n-card')[0].element).toBe(statusCard)
		expect(wrapper.text()).toContain('连接状态')
	})

	it('reports a failed region without emptying the page', async () => {
		const wrapper = await mountView({
			status: { token_present: true },
			networksError: new Error('网络读取失败'),
		})

		// The connection state is still shown even though the network list failed.
		expect(wrapper.text()).toContain('连接状态')
		expect(wrapper.text()).toContain('暂时无法从 Console 读取网络列表')
		// The failure must not appear together with the empty-list table: those are
		// two different answers to the same question.
		expect(wrapper.text()).not.toContain('该工作空间还没有网络')
	})

	// Opening the page while an install is already running has to follow it. The
	// screen and the download state come from two separate requests, so watching
	// only the screen missed this case entirely.
	it('follows an install that was already running when the page opened', async () => {
		const wrapper = await mountView({
			status: { core_installed: false, cli_installed: false, running: false },
			download: { state: 'running', phase: 'download', percent: 40 },
			// The status answer arrives first, so the screen is already the runtime
			// screen before the download state is known.
			downloadDelayMs: 120,
		})
		await new Promise((resolve) => setTimeout(resolve, 200))
		const afterMount = apiMock.downloadStatus.mock.calls.length

		await new Promise((resolve) => setTimeout(resolve, 1700))
		await flushPromises()

		expect(apiMock.downloadStatus.mock.calls.length).toBeGreaterThan(afterMount)
		expect(wrapper.text()).toContain('正在安装 EasyTier 运行时')
	})

	// Each service action reports what it did; restarting is not starting, and
	// the old page said so.
	it('reports a restart differently from a start', async () => {
		// Restarting is offered where the connection details could not be read.
		const wrapper = await mountView({ summaryError: new Error('读取失败') })
		apiMock.serviceAction.mockResolvedValue({})

		await wrapper.findAll('button').find((item) => item.text().includes('重启连接'))!.trigger('click')
		await flushPromises()
		expect(apiMock.serviceAction).toHaveBeenCalledWith('restart')
		expect(notifyMock).toHaveBeenCalledWith('操作已完成。', 'success')
	})

	// A component used in a template without being imported renders as an unknown
	// element, which produces a page that looks broken but raises no error. This
	// checks the whole view instead of trusting each file's imports.
	it('renders every component it uses', async () => {
		const wrapper = await mountView()
		const unresolved = wrapper.findAll('*')
			.map((node) => node.element.tagName.toLowerCase())
			.filter((tag) => tag.startsWith('n-'))
		expect(unresolved).toEqual([])
	})
})
