<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { NButton, NSelect, NSpace, NSpin } from 'naive-ui'
import { api } from '@/api/client'
import { ApiError, messageOf, needsRelogin } from '@/api/errors'
import { platform } from '@/platform'
import { notify } from '@/naive'
import { useResource } from '@/composables/useResource'
import { shared } from '@/stores/resources'
import { formatRemoteTime, networkName, nodeName, nodeRole, nodeStatus } from '@/utils/format'
import SectionCard from '@/components/SectionCard.vue'
import StatusBanner from '@/components/StatusBanner.vue'
import DetailList from '@/components/DetailList.vue'
import type { Network, Node } from '@/api/types'

// A read-only browser for the workspace's networks and their nodes. Joining and
// leaving happens on the overview page, so nothing here changes the device.

const router = useRouter()
// The catalogue is shared with the overview page; the node list belongs to the
// network selected here.
const { networks } = shared()
const nodes = useResource(() => api.networkNodes(selectedID.value || ''))

const selectedID = ref<string | null>(null)
const selectedNodeID = ref<string | null>(null)

const catalog = computed<Network[]>(() => networks.data.value?.networks || [])
const machineID = computed(() => networks.data.value?.machine_id || '')

const options = computed(() => catalog.value.map((network) => ({
	label: networkName(network),
	value: String(network.id),
})))

const nodeRows = computed<Node[]>(() => nodes.data.value?.nodes || [])

const selectedNode = computed<Node | null>(() => {
	const wanted = selectedNodeID.value
	if (!wanted) {
		return null
	}
	return nodeRows.value.find((node) => node.id === wanted) || null
})

const selectedNetwork = computed<Network | null>(() => {
	const wanted = selectedID.value
	return catalog.value.find((network) => network.id === wanted) || null
})

/** The failure that decides the whole page, in the daemon's own priority order. */
const blocked = computed(() => {
	const error = networks.error.value
	if (!error || networks.data.value) {
		return null
	}
	if (error instanceof ApiError && error.code === 'no_workspace') {
		return 'no-workspace'
	}
	if (error instanceof ApiError && error.code === 'not_authenticated') {
		return 'not-authenticated'
	}
	if (needsRelogin(error)) {
		return 'relogin'
	}
	return 'failed'
})

const nodeDetailEntries = computed(() => {
	const node = selectedNode.value
	if (!node) {
		return []
	}
	const isLocal = Boolean(machineID.value) && node.machine_id === machineID.value
	return [
		{ label: '主机名', value: node.hostname },
		{ label: 'DNS 名称', value: node.dns_name, mono: true },
		{ label: '状态', value: nodeStatus(node) },
		{ label: '系统', value: [ node.os, node.os_version ].filter(Boolean).join(' ') },
		{ label: 'EasyTier 版本', value: node.version, mono: true },
		{ label: '最后在线时间', value: formatRemoteTime(node.last_seen) },
		{ label: '所属', value: isLocal ? '本机（这台 NAS）' : '其他设备' },
	]
})

function isLocalNode(node: Node): boolean {
	return Boolean(machineID.value) && node.machine_id === machineID.value
}

async function reloadNodes(): Promise<void> {
	await nodes.reload()
}

// The first network is selected once the catalogue arrives, so the page shows
// something without a second click.
watch(catalog, (list) => {
	if (list.length === 0) {
		return
	}
	const known = list.some((network) => network.id === selectedID.value)
	if (!selectedID.value || !known) {
		selectedID.value = String(list[0].id)
	}
}, { immediate: true })

watch(selectedID, (value) => {
	selectedNodeID.value = null
	if (value) {
		void reloadNodes()
	}
})

onMounted(async () => {
	await networks.reload()
	if (blocked.value === 'failed') {
		notify(messageOf(networks.error.value), 'error')
	}
	// The watcher on the catalogue already loads the first network's nodes.
	if (selectedID.value) {
		void reloadNodes()
	}
})
</script>

<template>
	<div class="etp-stack">
		<SectionCard v-if="blocked === 'no-workspace'" title="网络" description="选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。">
			<StatusBanner type="warning">
				尚未选择 Console 工作空间，请先到「设置」页面完成工作空间配置。
			</StatusBanner>
			<n-space>
				<n-button @click="router.push({ name: 'settings' })">前往设置</n-button>
			</n-space>
		</SectionCard>

		<SectionCard v-else-if="blocked === 'not-authenticated'" title="网络" description="选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。">
			<StatusBanner type="warning">
				请先登录 EasyTier Console，登录入口在「概览」页面。
			</StatusBanner>
			<n-space>
				<n-button @click="router.push({ name: 'overview' })">前往概览</n-button>
			</n-space>
		</SectionCard>

		<SectionCard v-else-if="blocked === 'relogin'" title="网络">
			<StatusBanner type="warning">请重新登录 {{ platform.brandName }} 后再使用本页面。</StatusBanner>
		</SectionCard>

		<SectionCard v-else-if="blocked === 'failed'" title="网络" description="选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。">
			<StatusBanner type="error">
				无法读取工作空间网络：{{ messageOf(networks.error.value) }}
			</StatusBanner>
		</SectionCard>

		<SectionCard v-else-if="networks.settled.value && catalog.length === 0" title="网络" description="选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。">
			<StatusBanner type="info">
				当前工作空间还没有任何网络，请先在 EasyTier Console 网页控制台中创建网络。
			</StatusBanner>
		</SectionCard>

		<template v-else>
			<SectionCard title="网络" description="选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。">
				<StatusBanner v-if="networks.error.value" type="warning">
					<template v-if="needsRelogin(networks.error.value)">
						{{ platform.brandName }} 登录会话已失效，暂时无法刷新网络列表，下面是最后一次读取到的内容。
					</template>
					<template v-else>
						无法刷新网络列表：{{ messageOf(networks.error.value) }}。下面是最后一次读取到的内容。
					</template>
				</StatusBanner>
				<n-space v-if="networks.error.value">
					<n-button type="primary" tag="a" :href="platform.reloginURL" target="_top">重新登录 {{ platform.brandName }}</n-button>
					<n-button @click="networks.reload()">重试</n-button>
				</n-space>
				<div class="etp-row">
					<label class="etp-field">
						<span class="etp-field-label">网络</span>
						<n-select
							v-model:value="selectedID"
							:options="options"
							class="etp-select"
							:consistent-menu-width="false"
						/>
					</label>
					<n-button @click="reloadNodes">刷新</n-button>
				</div>
			</SectionCard>

			<SectionCard title="节点">
				<StatusBanner v-if="nodes.error.value && needsRelogin(nodes.error.value)" type="warning">
					请重新登录 {{ platform.brandName }} 后再使用本页面。
				</StatusBanner>
				<StatusBanner v-else-if="nodes.error.value" type="error">
					无法读取「{{ selectedNetwork ? networkName(selectedNetwork) : '所选网络' }}」的节点：{{ messageOf(nodes.error.value) }}
				</StatusBanner>

				<div v-if="nodes.loading.value && !nodes.settled.value" class="etp-loading">
					<n-spin size="small" />
					<span class="etp-muted">正在加载节点…</span>
				</div>

				<table v-else class="etp-table">
					<thead>
						<tr>
							<th>名称</th>
							<th>虚拟 IP</th>
							<th>状态</th>
							<th>角色</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="node in nodeRows" :key="String(node.id)">
							<td>
								<n-button text type="primary" @click="selectedNodeID = String(node.id)">
									<strong>{{ nodeName(node) }}</strong>{{ isLocalNode(node) ? '（本机）' : '' }}
								</n-button>
							</td>
							<td><span class="etp-mono">{{ node.ipv4_addr || '—' }}</span></td>
							<td>{{ nodeStatus(node) }}</td>
							<td>{{ nodeRole(node) }}</td>
						</tr>
						<tr v-if="nodeRows.length === 0">
							<td colspan="4" class="etp-muted">该网络下还没有节点。</td>
						</tr>
					</tbody>
				</table>
			</SectionCard>

			<SectionCard v-if="selectedNode" :title="`节点详情：${nodeName(selectedNode)}`">
				<DetailList :entries="nodeDetailEntries" />
			</SectionCard>
		</template>
	</div>
</template>

<style scoped>
.etp-field {
	display: flex;
	align-items: center;
	gap: 8px;
}

.etp-field-label {
	white-space: nowrap;
}

.etp-select {
	min-width: 240px;
}

.etp-loading {
	display: flex;
	align-items: center;
	gap: 10px;
	padding: 8px 0;
}
</style>
