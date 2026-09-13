<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { NAlert, NButton, NCode, NModal, NSpace } from 'naive-ui'
import { api } from '@/api/client'
import { messageOf, needsRelogin } from '@/api/errors'
import { notify } from '@/naive'
import { createTimerScope } from '@/utils/timers'
import type { AuthStartPayload } from '@/api/types'

// Device-code login: the Console returns a code the operator types into a
// browser page, and this dialog polls until that page reports success.

const props = defineProps<{ show: boolean }>()

const emit = defineEmits<{
	'update:show': [value: boolean]
	authenticated: []
	reloginRequired: []
}>()

const timerScope = createTimerScope()
const pending = ref(true)
const failure = ref<string | null>(null)
const start = ref<AuthStartPayload | null>(null)
let stopped = false

function close(): void {
	stopped = true
	timerScope.invalidate()
	emit('update:show', false)
}

function verificationTarget(payload: AuthStartPayload): string {
	return payload.verification_uri_complete || payload.verification_uri || ''
}

function poll(delay: number): void {
	if (stopped) {
		return
	}
	timerScope.after(Math.max(1, delay) * 1000, () => {
		api.authPoll().then((result) => {
			if (stopped) {
				return
			}
			if (result.authenticated) {
				close()
				notify('已成功登录 EasyTier Console。', 'success')
				emit('authenticated')
				return
			}
			poll(Number(result.retry_after) || 5)
		}).catch((error) => {
			if (stopped) {
				return
			}
			stopped = true
			close()
			if (needsRelogin(error)) {
				notify('请重新登录 DSM 后再试。', 'error')
				emit('reloginRequired')
				return
			}
			failure.value = messageOf(error)
		})
	})
}

function begin(): void {
	stopped = false
	timerScope.invalidate()
	pending.value = true
	failure.value = null
	start.value = null
	api.authStart().then((result) => {
		if (stopped) {
			return
		}
		pending.value = false
		start.value = result
		poll(Number(result.interval) || 5)
	}).catch((error) => {
		if (stopped) {
			return
		}
		stopped = true
		close()
		notify(messageOf(error), 'error')
	})
}

watch(() => props.show, (visible) => {
	if (visible) {
		begin()
	} else {
		stopped = true
		timerScope.invalidate()
	}
}, { immediate: true })

onUnmounted(() => {
	stopped = true
	timerScope.invalidate()
})
</script>

<template>
	<n-modal
		:show="show"
		preset="card"
		title="登录 EasyTier Console"
		class="etp-dialog"
		:mask-closable="false"
		:close-on-esc="false"
		@update:show="close"
	>
		<div class="etp-stack">
			<p v-if="pending" class="etp-paragraph">正在向 Console 申请设备码…</p>

			<template v-else-if="start">
				<p class="etp-paragraph">请在浏览器中打开下面的授权页面，并输入验证码完成登录。</p>
				<p class="etp-row">
					<strong>验证码：</strong>
					<n-code :code="String(start.user_code || '')" />
				</p>
				<p class="etp-paragraph">
					<a
						v-if="verificationTarget(start)"
						:href="verificationTarget(start)"
						target="_blank"
						rel="noreferrer noopener"
					>{{ start.verification_uri || verificationTarget(start) }}</a>
				</p>
				<p class="etp-paragraph etp-muted">页面会自动检测登录结果，完成后此窗口将自动关闭。</p>
			</template>

			<n-alert v-if="failure" type="warning" :title="failure">
				<p class="etp-paragraph">可以重新开始设备登录，或稍后重试。</p>
			</n-alert>
		</div>

		<template #footer>
			<n-space>
				<n-button @click="close">取消</n-button>
				<n-button
					v-if="!failure"
					type="primary"
					:disabled="pending || !start"
					tag="a"
					:href="start ? verificationTarget(start) : undefined"
					target="_blank"
					rel="noopener"
				>打开授权页面</n-button>
				<n-button v-else type="primary" @click="begin">重新开始</n-button>
			</n-space>
		</template>
	</n-modal>
</template>
