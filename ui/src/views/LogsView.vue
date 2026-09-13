<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { NButton, NCollapse, NCollapseItem, NSelect, NSpace, NSpin } from 'naive-ui'
import { api } from '@/api/client'
import { messageOf, needsRelogin } from '@/api/errors'
import { notify } from '@/naive'
import { useResource } from '@/composables/useResource'
import { logLineCount, refreshStatus, shared } from '@/stores/resources'
import { createTimerScope } from '@/utils/timers'
import { interfaceNames } from '@/utils/summary'
import SectionCard from '@/components/SectionCard.vue'
import StatusBanner from '@/components/StatusBanner.vue'
import DetailList from '@/components/DetailList.vue'

// Diagnostics: what the connection is doing, the controls for it, and the
// daemon's recent log. The log tail is the only place an operator can see why
// something failed, so it is kept on the page rather than behind a download.

const LINE_OPTIONS = [ 100, 200, 500 ]
const EMPTY_LOGS = '暂无匹配的日志'

// The chosen line count lives in the shared state, so it survives navigation.
const lineCount = logLineCount
const lineOptions = LINE_OPTIONS.map((count) => ({ label: `${count} 行`, value: count }))

const timerScope = createTimerScope()
// Status and summary are shared, so the page opens with the values it last saw.
const { status, summary } = shared()
const logs = useResource(() => api.logs(lineCount.value))

const actionInFlight = ref(false)
const refreshingLogs = ref(false)

const running = computed(() => Boolean(status.data.value?.running))
const machineID = computed(() => status.data.value?.machine_id || '')

const statusEntries = computed(() => {
	const value = status.data.value || {}
	return [
		{ label: '连接服务', value: value.running ? '运行中' : '已停止' },
		{ label: '隧道接口', value: interfaceNames(summary.data.value?.interfaces).join(', ') || '暂无' },
		{ label: 'Core 版本', value: value.core_version, mono: true },
		{ label: 'CLI 版本', value: value.cli_version, mono: true },
		{ label: '本机 ID', value: machineID.value || undefined, mono: true },
		{ label: '工作空间', value: value.workspace_id },
		{ label: '配置服务器', value: value.config_server, mono: true },
	]
})

const logText = computed(() => {
	const text = logs.data.value?.logs || ''
	return text.trim() === '' ? EMPTY_LOGS : text
})

// Only the first failure is reported: the three reads run together, and the
// status one is the most informative.
const loadError = computed(() => status.error.value || summary.error.value || logs.error.value)

function reportError(error: unknown): void {
	if (needsRelogin(error)) {
		notify('登录状态已失效，请重新登录 DSM 后再试。', 'error')
		return
	}
	notify(messageOf(error) || '操作失败，请稍后重试。', 'error')
}

function runServiceAction(action: 'start' | 'stop' | 'restart'): void {
	if (actionInFlight.value) {
		return
	}
	actionInFlight.value = true
	api.serviceAction(action).then(() => {
		const text = action === 'stop'
			? '连接已停止。'
			: (action === 'start' ? '连接已启动，正在刷新状态…' : '连接服务已重启，正在刷新状态…')
		notify(text, 'success')
		// Give the service a moment to come up before asking for its state.
		timerScope.after(1200, () => {
			actionInFlight.value = false
			void Promise.all([ status.reload(), summary.reload() ])
		})
	}).catch((error) => {
		actionInFlight.value = false
		reportError(error)
		void refresh()
	})
}

async function refresh(): Promise<void> {
	await status.reload()
	await summary.reload()
}

async function refreshLogs(): Promise<void> {
	refreshingLogs.value = true
	try {
		await logs.reload()
	} catch {
		// The failure is rendered from the resource's error state.
	} finally {
		refreshingLogs.value = false
	}
}

onMounted(() => {
	void Promise.all([ refreshStatus(), summary.reload(), logs.reload() ])
	if (loadError.value) {
		reportError(loadError.value)
	}
})

onUnmounted(() => {
	timerScope.invalidate()
})
</script>

<template>
	<div class="etp-stack">
		<StatusBanner v-if="loadError" type="error">
			{{ needsRelogin(loadError) ? '登录状态已失效，请重新登录 DSM 后再试。' : messageOf(loadError) }}
		</StatusBanner>

		<SectionCard title="连接状态" description="连接异常时，可先核对下面的状态，再尝试重启连接。" :loading="status.loading.value && status.settled.value">
			<div v-if="status.loading.value && !status.settled.value" class="etp-loading">
				<n-spin size="small" />
				<span class="etp-muted">正在读取状态…</span>
			</div>
			<DetailList v-else :entries="statusEntries" />
			<n-space>
				<n-button type="primary" :disabled="actionInFlight" @click="runServiceAction('restart')">重启连接</n-button>
				<n-button
					v-if="running"
					type="error"
					ghost
					:disabled="actionInFlight"
					@click="runServiceAction('stop')"
				>停止连接</n-button>
				<n-button v-else type="primary" :disabled="actionInFlight" @click="runServiceAction('start')">启动连接</n-button>
			</n-space>
		</SectionCard>

		<SectionCard title="最近日志" description="连接出问题时，可在这里查看本机服务的最近记录。">
			<div class="etp-row">
				<span class="etp-muted">显示行数</span>
				<n-select v-model:value="lineCount" :options="lineOptions" aria-label="显示行数" class="etp-lines" />
				<n-button :loading="refreshingLogs" :disabled="refreshingLogs" @click="refreshLogs">刷新</n-button>
			</div>
			<StatusBanner v-if="logs.error.value" type="error">
				日志加载失败，请点击“刷新”重试。
			</StatusBanner>
			<pre class="etp-log-output">{{ logText }}</pre>
		</SectionCard>

		<SectionCard>
			<n-collapse>
				<n-collapse-item title="关于诊断数据" name="privacy">
					<p class="etp-paragraph etp-muted">
						日志最多显示最近 64 KB 的内容。设备注册令牌、授权请求头和类似令牌的内容会在发送到浏览器之前自动移除，不会显示在这里。
					</p>
				</n-collapse-item>
			</n-collapse>
		</SectionCard>
	</div>
</template>

<style scoped>
.etp-lines {
	width: 120px;
}

.etp-loading {
	display: flex;
	align-items: center;
	gap: 10px;
	padding: 8px 0;
}
</style>
