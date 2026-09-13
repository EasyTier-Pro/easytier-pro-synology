<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NCollapse, NCollapseItem, NProgress, NSpace, NSpin } from 'naive-ui'
import { api } from '@/api/client'
import { messageOf, needsRelogin } from '@/api/errors'
import { confirmDestructive, notify } from '@/naive'
import { showOperationProgress } from '@/composables/useOperationProgress'
import { refreshStatus, shared } from '@/stores/resources'
import { createTimerScope } from '@/utils/timers'
import { downloadMessage, percentOf } from '@/utils/phases'
import { interfaceNames, localIPv4, peerCount } from '@/utils/summary'
import { networkName } from '@/utils/format'
import SectionCard from '@/components/SectionCard.vue'
import StatusBanner from '@/components/StatusBanner.vue'
import DetailList from '@/components/DetailList.vue'
import AccountActions from '@/components/AccountActions.vue'
import BindNotice from '@/components/BindNotice.vue'
import TunNotice from '@/components/TunNotice.vue'
import DeviceLoginDialog from '@/components/DeviceLoginDialog.vue'
import TokenDialog from '@/components/TokenDialog.vue'
import WorkspaceDialog from '@/components/WorkspaceDialog.vue'
import EnrollmentDialog from '@/components/EnrollmentDialog.vue'
import type {
	EnrollmentOptions,
	Membership,
	Network,
	Operation,
	Workspace,
} from '@/api/types'

const router = useRouter()
const timerScope = createTimerScope()
const { after } = timerScope

// Declared before the watchers below, because an immediate watcher starts the
// install poll during setup.
let downloadPolling = false
let disposed = false

// Every region loads on its own, so an action can refresh just the part it
// changed and the rest of the page keeps what it is showing. The resources are
// shared between pages, so opening this one renders from the last read instead
// of showing a placeholder again.
const { status, auth, download, summary, networks } = shared()

const deviceLoginOpen = ref(false)
const tokenOpen = ref(false)
const workspaceOpen = ref(false)
const enrollmentOpen = ref(false)
const workspaces = ref<Workspace[]>([])
const enrollmentWorkspace = ref<Workspace | null>(null)
const enrollmentOptions = ref<EnrollmentOptions | null>(null)
const enrollmentLoading = ref(false)

const loggedIn = computed(() => Boolean(auth.data.value?.logged_in))

// A read that fails while earlier data is still on screen. The data is kept -
// it is the last known state and better than an empty page - but the failure has
// to be visible, and an expired DSM session has to be renewable from here.
const sessionFailure = computed(() => status.error.value || auth.error.value)
const origin = window.location.origin
const current = computed(() => status.data.value)

// The screen the page shows, derived rather than assigned: a reload of any
// region recomputes it, and there is no separate state to keep in step.
const screen = computed(() => {
	if (status.error.value && !status.data.value) {
		return 'unavailable'
	}
	if (!status.data.value) {
		return 'loading'
	}
	const value = status.data.value
	if (!loggedIn.value && !value.token_present) {
		return 'first-use'
	}
	// A device token alone is a complete setup: only users who have not
	// connected yet need to choose a workspace.
	if (!value.token_present) {
		return 'workspace'
	}
	if (!value.core_installed || !value.cli_installed) {
		return 'runtime'
	}
	if (!value.running) {
		return 'stopped'
	}
	return 'running'
})

const downloadRunning = computed(() => {
	const state = download.data.value?.state
	return state === 'queued' || state === 'running'
})

const downloadPercent = computed(() => percentOf(download.data.value?.percent))

const joined = computed<Membership[]>(() => {
	const machine = networks.data.value?.machine
	return machine && Array.isArray(machine.networks) ? machine.networks : []
})

/** The network list, including memberships the catalogue does not carry yet. */
const networkRows = computed(() => {
	const memberships = new Map<string, Membership>()
	for (const item of joined.value) {
		if (item && item.id) {
			memberships.set(item.id, item)
		}
	}
	const catalog: Network[] = [ ...(networks.data.value?.networks || []) ]
	const known = new Set(catalog.map((network) => network.id))
	for (const item of joined.value) {
		if (item && item.id && !known.has(item.id)) {
			// A membership names its network with `name` and carries the subnet
			// too; the catalogue is what names it `network_name`, so both spellings
			// are copied. Without the name this entry would list as a bare uuid.
			catalog.push({
				id: item.id,
				name: item.network_name || (typeof item.name === 'string' ? item.name : ''),
				ipv4_cidr: typeof item.ipv4_cidr === 'string' ? item.ipv4_cidr : '',
			})
		}
	}
	return catalog.map((network) => ({
		network,
		membership: network.id ? memberships.get(network.id) || null : null,
	}))
})

const registering = computed(() => {
	const payload = networks.data.value
	return Boolean(payload && !payload.enrolled && networkRows.value.length === 0)
})

// One state per region, so its branches are mutually exclusive by construction.
// A failed first load and a successful one used to be able to render together.
const summaryState = computed(() => {
	if (summary.data.value) {
		return 'ready'
	}
	return summary.settled.value ? 'unavailable' : 'loading'
})

const networkState = computed(() => {
	if (networks.data.value) {
		return registering.value ? 'registering' : 'ready'
	}
	return networks.settled.value ? 'unavailable' : 'loading'
})

const detailEntries = computed(() => {
	const value = current.value || {}
	const info = summary.data.value
	return [
		{
			label: '运行模式',
			value: value.tun_capable ? '完整模式（本机有虚拟网卡）' : '无 TUN 模式（用户态转发）',
		},
		{ label: '本机虚拟 IP', value: localIPv4(info) || undefined, mono: true },
		{ label: 'Peer 数', value: String(peerCount(info && info.peers)) },
		{
			label: '虚拟网卡',
			value: interfaceNames(info && info.interfaces).join(', ')
				|| (value.tun_capable ? undefined : '无（无 TUN 模式）'),
		},
		{ label: 'Core 版本', value: value.core_version },
		{ label: 'CLI 版本', value: value.cli_version },
	]
})

/** refresh reloads the regions one action can have changed. */
async function refresh(...kinds: Array<'status' | 'auth' | 'download' | 'summary' | 'networks'>): Promise<void> {
	await Promise.all(kinds.map((kind) => {
		switch (kind) {
			case 'status': return status.reload()
			case 'auth': return auth.reload()
			case 'download': return download.reload()
			case 'summary': return summary.reload()
			case 'networks': return networks.reload()
		}
	}))
}

// The running screen needs the runtime summary and the network list, and they
// are only meaningful once the core is up.
// The running screen's regions are reloaded whenever that screen is reached,
// including when it is already the current one as the page mounts: the shared
// status may be cached from an earlier visit, in which case there is no
// transition to watch for and the regions would never be read. Reloading keeps
// the previous values visible, so this costs no flicker.
watch(screen, (value) => {
	if (value === 'running') {
		void summary.reload()
		void networks.reload()
	}
}, { immediate: true })

// Polling has to follow both the screen and the download state: they arrive from
// two requests, so a page opened while an install is already running would
// otherwise never start following it.
const followDownload = computed(() => screen.value === 'runtime' && downloadRunning.value)
watch(followDownload, (active) => {
	if (active) {
		pollDownload()
	}
}, { immediate: true })

/**
 * pollDownload follows an install in the background while this page is open.
 *
 * Every continuation checks `disposed` first: the timer scope abandons scheduled
 * work, but a request already in flight still resolves, and continuing from it
 * would schedule the next tick into the new mount's generation and poll forever
 * on a page nobody is looking at.
 */
function pollDownload(): void {
	if (downloadPolling || disposed) {
		return
	}
	downloadPolling = true
	const stop = (): void => {
		downloadPolling = false
	}
	const tick = (): void => {
		if (disposed) {
			stop()
			return
		}
		api.downloadStatus().then((value) => {
			if (disposed) {
				stop()
				return
			}
			download.set(value)
			if (value.state === 'queued' || value.state === 'running') {
				after(1500, tick)
				return
			}
			stop()
			if (value.state === 'completed') {
				notify('运行时安装完成。', 'success')
				void refresh('status', 'auth')
			}
		}).catch(() => {
			if (disposed) {
				stop()
				return
			}
			// 状态查询失败时继续稍后重试，安装本身在后台进行。
			after(3000, tick)
		})
	}
	after(1500, tick)
}

function startDownload(): void {
	api.downloadStart().then(() => {
		notify('已开始下载并安装运行时。', 'success')
		// Reading the new state is enough: the watcher above starts polling.
		void refresh('status', 'download')
	}).catch((error) => {
		if (error && (error as { code?: string }).code === 'download_busy') {
			void refresh('status', 'download')
			return
		}
		notify(messageOf(error), 'error')
	})
}

// Starting and restarting take a moment, so the button is held disabled while
// the request is in flight: a second click would ask for the same change again.
const serviceActionInFlight = ref(false)

function serviceAction(action: 'start' | 'stop' | 'restart'): void {
	if (serviceActionInFlight.value) {
		return
	}
	serviceActionInFlight.value = true
	api.serviceAction(action).then(() => {
		notify(action === 'restart' ? '操作已完成。' : '连接已启动。', 'success')
		void refresh('status', 'summary', 'networks').finally(() => {
			serviceActionInFlight.value = false
		})
	}).catch((error) => {
		serviceActionInFlight.value = false
		notify(messageOf(error), 'error')
	})
}

async function confirmLeave(network: Network): Promise<void> {
	const agreed = await confirmDestructive({
		title: `退出「${networkName(network)}」？`,
		body: '本机将退出该网络，其它已加入的网络不受影响。',
		positiveText: '退出网络',
	})
	if (!agreed) {
		return
	}
	api.networkLeave(network.id || '').then((result) => {
		showOperationProgress(`正在退出「${networkName(network)}」`, result.operation_id || '', {
			completed: () => {
				notify(`本机已退出「${networkName(network)}」。`, 'success')
				void refresh('networks', 'summary')
			},
			failed: () => void refresh('networks', 'summary'),
		}, timerScope)
	}).catch((error) => notify(messageOf(error), 'error'))
}

function joinNetwork(network: Network): void {
	api.networkJoin(network.id || '').then((result) => {
		showOperationProgress(`正在加入「${networkName(network)}」`, result.operation_id || '', {
			completed: () => {
				notify(`本机已加入「${networkName(network)}」。`, 'success')
				void refresh('networks', 'summary')
			},
			failed: () => void refresh('networks', 'summary'),
		}, timerScope)
	}).catch((error) => notify(messageOf(error), 'error'))
}

async function confirmLogout(): Promise<void> {
	const agreed = await confirmDestructive({
		title: '退出 Console 登录？',
		body: '只会清除本机保存的 Console 登录状态，已配置的连接和正在运行的隧道不受影响。',
		positiveText: '退出登录',
	})
	if (!agreed) {
		return
	}
	api.authLogout().then(() => {
		notify('已退出 Console 登录。', 'success')
		void refresh('status', 'auth')
	}).catch((error) => notify(messageOf(error), 'error'))
}

async function confirmDisconnect(): Promise<void> {
	const agreed = await confirmDestructive({
		title: '断开本机？',
		body: '将停止本机的 EasyTier 服务并删除保存的设备令牌；Console 登录状态会保留。之后需要重新连接才能再次加入网络。',
		positiveText: '断开本机',
	})
	if (!agreed) {
		return
	}
	api.disconnect().then((result) => {
		showOperationProgress('正在断开本机', result.operation_id || '', {
			completed: () => {
				notify('本机已断开连接。', 'success')
				void refresh('status', 'auth')
			},
			failed: () => void refresh('status', 'auth'),
		}, timerScope)
	}).catch((error) => notify(messageOf(error), 'error'))
}

function submitToken(token: string, configServer: string): void {
	api.connectToken(token, configServer).then((result) => {
		showOperationProgress('正在连接本机', result.operation_id || '', {
			completed: (operation: Operation) => {
				notify(operation.needs_download ? '令牌已保存，接下来需要安装运行时。' : '本机已开始连接。', 'success')
				void refresh('status', 'auth')
			},
			failed: () => void refresh('status', 'auth'),
		}, timerScope)
	}).catch((error) => notify(messageOf(error), 'error'))
}

async function openWorkspaceDialog(): Promise<void> {
	try {
		const account = await api.authMe()
		const tenants = account.tenants || []
		if (tenants.length === 0) {
			notify('当前账号没有可用的工作空间。', 'error')
			return
		}
		if (tenants.length === 1) {
			await loadEnrollmentOptions(tenants[0])
			return
		}
		workspaces.value = tenants
		workspaceOpen.value = true
	} catch (error) {
		notify(messageOf(error), 'error')
	}
}

async function loadEnrollmentOptions(workspace: Workspace): Promise<void> {
	enrollmentWorkspace.value = workspace
	enrollmentLoading.value = true
	enrollmentOpen.value = true
	enrollmentOptions.value = null
	try {
		enrollmentOptions.value = await api.enrollmentOptions(workspace.id || workspace.slug || '')
	} catch (error) {
		enrollmentOpen.value = false
		notify(messageOf(error), 'error')
	} finally {
		enrollmentLoading.value = false
	}
}

function activate(mode: string, keyID: string): void {
	const workspace = enrollmentWorkspace.value
	if (!workspace) {
		return
	}
	api.activate(workspace.id || workspace.slug || '', mode, keyID).then((result) => {
		showOperationProgress('正在完成本机设置', result.operation_id || '', {
			completed: (operation: Operation) => {
				notify(operation.needs_download ? '注册完成，接下来需要安装运行时。' : '本机设置已完成。', 'success')
				void refresh('status', 'auth')
			},
			failed: () => void refresh('status', 'auth'),
		}, timerScope)
	}).catch((error) => notify(messageOf(error), 'error'))
}

onMounted(() => {
	// A reload keeps what is already known visible, so returning to this page
	// does not blank it while the answer is on the way.
	void refreshStatus()
})

onUnmounted(() => {
	disposed = true
	timerScope.invalidate()
})
</script>

<template>
	<div class="etp-stack">
		<!-- A refresh failed while earlier data is still shown. -->
		<StatusBanner
			v-if="sessionFailure && screen !== 'unavailable'"
			:type="needsRelogin(sessionFailure) ? 'warning' : 'error'"
		>
			<p v-if="needsRelogin(sessionFailure)" class="etp-paragraph">
				本机与 DSM 的登录会话已失效，暂时无法刷新，下面显示的是最后一次读取到的状态。
			</p>
			<p v-else class="etp-paragraph">
				{{ messageOf(sessionFailure) }}。下面显示的是最后一次读取到的状态。
			</p>
			<n-space>
				<n-button type="primary" tag="a" href="/webman/index.cgi" target="_blank">重新登录 DSM</n-button>
				<n-button @click="refresh('status', 'auth', 'download')">重试</n-button>
			</n-space>
		</StatusBanner>

		<!-- Unavailable: the daemon could not be asked, which is usually the DSM session. -->
		<template v-if="screen === 'unavailable'">
			<SectionCard
				v-if="needsRelogin(status.error.value)"
				title="需要有效的 DSM 登录会话"
			>
				<StatusBanner type="warning">
					<p class="etp-paragraph">本应用没有拿到有效的 DSM 登录会话，因此无法读取本机状态。</p>
					<p class="etp-paragraph">
						常见原因有两个：DSM 登录其实没有成功；或者登录 DSM 用的地址与本应用的地址不同——DSM
						的会话按地址区分，在别的地址登录不会让本应用生效。
					</p>
					<p class="etp-paragraph etp-muted">本应用使用的地址：{{ origin }}</p>
				</StatusBanner>
				<n-space>
					<n-button type="primary" tag="a" href="/webman/index.cgi" target="_blank">重新登录 DSM</n-button>
					<n-button @click="refresh('status', 'auth', 'download')">已确认登录，重试</n-button>
				</n-space>
			</SectionCard>

			<SectionCard v-else title="暂时无法读取本机状态">
				<StatusBanner type="warning">
					{{ messageOf(status.error.value) }}。本机的连接设置没有被修改。
				</StatusBanner>
				<n-space>
					<n-button type="primary" @click="refresh('status', 'auth', 'download')">重试</n-button>
					<n-button tag="a" @click="router.push({ name: 'logs' })">查看日志</n-button>
				</n-space>
			</SectionCard>
		</template>

		<!-- First use: no account and no device token yet. -->
		<template v-else-if="screen === 'first-use'">
			<SectionCard title="首次使用" description="本机尚未连接 EasyTier">
				<p class="etp-paragraph">把这台 NAS 接入 EasyTier 虚拟网络，只需完成下面三步。</p>
				<ol class="etp-step-list">
					<li><strong>登录</strong><br>使用 EasyTier Console 账号完成设备登录。</li>
					<li><strong>准备</strong><br>自动下载并安装所需的连接组件。</li>
					<li><strong>连接</strong><br>选择要让本机加入的私有网络。</li>
				</ol>
				<n-space>
					<n-button type="primary" @click="deviceLoginOpen = true">开始使用</n-button>
				</n-space>
			</SectionCard>

			<SectionCard title="高级方式" description="不使用 Console 账号的连接方式">
				<n-collapse>
					<n-collapse-item title="高级设置" name="advanced">
						<p class="etp-paragraph etp-muted">
							已经持有设备注册令牌？可以不登录账号，直接让本机接入网络。
						</p>
						<n-button @click="tokenOpen = true">使用设备令牌</n-button>
					</n-collapse-item>
				</n-collapse>
			</SectionCard>
		</template>

		<!-- Workspace: a token exists, but the workspace was never chosen. -->
		<template v-else-if="screen === 'workspace'">
			<StatusBanner v-if="loggedIn" type="success">已登录 EasyTier Console。</StatusBanner>
			<StatusBanner v-else type="info">本机已保存设备令牌，但还没有完成工作空间设置。</StatusBanner>
			<SectionCard title="完成本机设置" description="账号已连接，还差最后一步">
				<p class="etp-paragraph">
					请选择本机所属的 Console 工作空间，并决定使用哪一个注册密钥，其余设置会自动完成。
				</p>
				<n-space>
					<n-button type="primary" @click="openWorkspaceDialog">继续设置</n-button>
					<n-button @click="confirmLogout">退出 Console 登录</n-button>
				</n-space>
			</SectionCard>
		</template>

		<!-- Runtime: the connection components are not installed yet. -->
		<template v-else-if="screen === 'runtime'">
			<SectionCard
				:title="downloadRunning ? '正在安装 EasyTier 运行时' : '安装 EasyTier 运行时'"
				description="本机准备工作"
			>
				<template v-if="downloadRunning">
					<div class="etp-spread">
						<strong>{{ downloadMessage(download.data.value) || '正在准备…' }}</strong>
						<span class="etp-mono">{{ downloadPercent }}%</span>
					</div>
					<n-progress :percentage="downloadPercent" :show-indicator="false" />
					<StatusBanner type="info">
						进度会自动更新；安装在本机后台进行，可以放心离开本页面稍后再回来。
					</StatusBanner>
				</template>
				<template v-else>
					<StatusBanner v-if="download.data.value?.state === 'failed'" type="warning">
						{{ downloadMessage(download.data.value) || '安装未完成。' }} 账号与本机设置都已保留。
					</StatusBanner>
					<p v-else class="etp-paragraph">
						运行 EasyTier 连接服务前，需要先在本机安装一次运行时。系统会自动下载正确的版本并校验后再安装。
					</p>
					<n-space>
						<n-button type="primary" @click="startDownload">
							{{ download.data.value?.state === 'failed' ? '重试安装' : '安装并继续' }}
						</n-button>
					</n-space>
				</template>
				<AccountActions :status="current || {}" :logged-in="loggedIn" @logout="confirmLogout" @disconnect="confirmDisconnect" />
			</SectionCard>
		</template>

		<!-- Stopped: installed and enrolled, waiting to be started. -->
		<template v-else-if="screen === 'stopped'">
			<SectionCard title="启动连接" description="本机已准备就绪">
				<BindNotice :status="current || {}" />
				<TunNotice :status="current || {}" />
				<p class="etp-paragraph">
					EasyTier 已安装，本机也已与账号关联。启动连接后，本机就会加入所选的私有网络。
				</p>
				<n-space>
					<n-button type="primary" :disabled="serviceActionInFlight" @click="serviceAction('start')">启动连接</n-button>
				</n-space>
				<AccountActions :status="current || {}" :logged-in="loggedIn" @logout="confirmLogout" @disconnect="confirmDisconnect" />
			</SectionCard>
		</template>

		<!-- Running: the normal state. -->
		<template v-else-if="screen === 'running'">
			<SectionCard title="连接状态" :loading="summary.loading.value && summary.settled.value">
				<BindNotice :status="current || {}" />
				<TunNotice :status="current || {}" />
				<StatusBanner v-if="summaryState === 'unavailable'" type="warning">
					无法读取本机连接详情，连接服务可能刚启动或已停止。可以稍后刷新，或先重启连接。
				</StatusBanner>
				<div v-else-if="summaryState === 'loading'" class="etp-loading">
					<n-spin size="small" />
					<span class="etp-muted">正在读取本机连接状态…</span>
				</div>
				<DetailList v-else :entries="detailEntries" />
				<n-space v-if="summaryState === 'unavailable'">
					<n-button @click="refresh('status', 'summary', 'networks')">刷新</n-button>
					<n-button :disabled="serviceActionInFlight" @click="serviceAction('restart')">重启连接</n-button>
				</n-space>
			</SectionCard>

			<SectionCard title="网络" :loading="networks.loading.value && networks.settled.value">
				<template v-if="networkState === 'unavailable'">
					<StatusBanner type="warning">
						暂时无法从 Console 读取网络列表；本机已建立的隧道不受影响。
					</StatusBanner>
					<n-space>
						<n-button @click="refresh('networks', 'summary')">重试</n-button>
					</n-space>
				</template>

				<template v-else-if="networkState === 'loading'">
					<div class="etp-loading">
						<n-spin size="small" />
						<span class="etp-muted">正在读取网络列表…</span>
					</div>
				</template>

				<template v-else-if="networkState === 'registering'">
					<StatusBanner type="info">
						本机正在向 Console 注册，网络列表稍后才会出现，请稍候刷新。
					</StatusBanner>
					<n-space>
						<n-button @click="refresh('networks')">刷新</n-button>
					</n-space>
				</template>

				<template v-else>
					<p class="etp-paragraph etp-muted">可以分别加入或退出每个网络，互不影响。</p>
					<table class="etp-table">
						<thead>
							<tr>
								<th>名称</th>
								<th>IPv4 段</th>
								<th>本机状态</th>
								<th>操作</th>
							</tr>
						</thead>
						<tbody>
							<tr v-for="row in networkRows" :key="String(row.network.id)">
								<td><strong>{{ networkName(row.network) }}</strong></td>
								<td>
									<span v-if="row.network.ipv4_cidr" class="etp-mono">{{ row.network.ipv4_cidr }}</span>
									<span v-else>—</span>
								</td>
								<td>
									<span v-if="row.membership" class="etp-mono">
										{{ row.membership.node_ipv4 || row.membership.ipv4_addr || row.membership.virtual_ip || '已加入' }}
									</span>
									<span v-else class="etp-muted">未加入</span>
								</td>
								<td>
									<n-button
										v-if="row.membership"
										size="small"
										type="error"
										ghost
										@click="confirmLeave(row.network)"
									>退出 {{ networkName(row.network) }}</n-button>
									<n-button v-else size="small" type="primary" @click="joinNetwork(row.network)">
										加入 {{ networkName(row.network) }}
									</n-button>
								</td>
							</tr>
							<tr v-if="networkRows.length === 0">
								<td colspan="4" class="etp-muted">该工作空间还没有网络，可先在 Console 网页中创建。</td>
							</tr>
						</tbody>
					</table>
				</template>
			</SectionCard>

			<SectionCard title="账号与本机">
				<AccountActions :status="current || {}" :logged-in="loggedIn" @logout="confirmLogout" @disconnect="confirmDisconnect" />
			</SectionCard>
		</template>

		<SectionCard v-else title="正在加载">
			<n-spin size="small" />
		</SectionCard>

		<DeviceLoginDialog
			v-model:show="deviceLoginOpen"
			@authenticated="refresh('status', 'auth')"
			@relogin-required="refresh('status', 'auth')"
		/>
		<TokenDialog
			v-model:show="tokenOpen"
			@submit="submitToken"
			@invalid="notify('请输入设备令牌。', 'error')"
		/>
		<WorkspaceDialog
			v-model:show="workspaceOpen"
			:workspaces="workspaces"
			@select="loadEnrollmentOptions"
		/>
		<EnrollmentDialog
			v-model:show="enrollmentOpen"
			:workspace-name="enrollmentWorkspace ? (enrollmentWorkspace.name || enrollmentWorkspace.slug || '') : ''"
			:options="enrollmentLoading ? null : enrollmentOptions"
			@activate="activate"
		/>
	</div>
</template>
