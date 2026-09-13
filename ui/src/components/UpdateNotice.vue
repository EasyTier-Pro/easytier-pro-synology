<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NProgress, NSpace } from 'naive-ui'
import StatusBanner from './StatusBanner.vue'
import { downloadMessage, percentOf } from '@/utils/phases'
import type { DownloadStatus, Status } from '@/api/types'

/* 核心装好之后，这个提示是「有新版本」唯一的入口：安装运行时那个画面只在核心缺失时
   才出现，所以升级必须在这里说。

   升级会重启核心、短暂中断隧道，因此由用户点确认，后台不自动升级。 */
const props = defineProps<{
	status: Status
	download: DownloadStatus | null
	/** Disables the button while an upgrade is already on its way. */
	busy?: boolean
}>()

const emit = defineEmits<{ upgrade: [] }>()

const running = computed(() => {
	const state = props.download?.state
	return state === 'queued' || state === 'running'
})

const failed = computed(() => props.download?.state === 'failed')

// Only shown once the Console has told us about a release and it differs from
// what is installed; the daemon decides that, so the two never disagree.
const available = computed(() => Boolean(props.status.core_update_available))

const version = computed(() => props.status.latest_version || '')

const percent = computed(() => percentOf(props.download?.percent))

const visible = computed(() => available.value || running.value)
</script>

<template>
	<StatusBanner v-if="visible" :type="failed ? 'warning' : 'info'">
		<template v-if="running">
			<p class="etp-paragraph">正在安装 EasyTier {{ version }}…</p>
			<div class="etp-spread">
				<span>{{ downloadMessage(download) || '正在准备…' }}</span>
				<span class="etp-mono">{{ percent }}%</span>
			</div>
			<n-progress :percentage="percent" :show-indicator="false" />
			<p class="etp-paragraph etp-muted">
				升级会重启连接服务，期间网络会短暂中断；本机已加入的网络不会丢失。
			</p>
		</template>

		<template v-else>
			<p class="etp-paragraph">EasyTier 有新版本 {{ version }} 可用。</p>
			<p v-if="failed" class="etp-paragraph etp-muted">
				上次安装未完成：{{ downloadMessage(download) || '安装未完成。' }}
			</p>
			<p v-else class="etp-paragraph etp-muted">
				升级会重启连接服务，网络会短暂中断；本机已加入的网络不会丢失。
			</p>
			<n-space>
				<n-button type="primary" size="small" :disabled="busy" @click="emit('upgrade')">
					{{ failed ? '重试升级' : '升级' }}
				</n-button>
			</n-space>
		</template>
	</StatusBanner>
</template>
