<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { NAlert, NButton, NModal, NRadio, NSpace } from 'naive-ui'
import { buildEnrollmentChoices } from '@/utils/enrollment'
import type { EnrollmentOptions } from '@/api/types'

// Choosing how this device enrols: reuse what exists, or create a new key. Every
// choice is explained where it is offered, because the difference between a
// shared and a dedicated key is the difference between who else is affected when
// it is revoked later.

const props = defineProps<{
	show: boolean
	workspaceName: string
	options: EnrollmentOptions | null
}>()

const emit = defineEmits<{
	'update:show': [value: boolean]
	activate: [mode: string, keyID: string]
}>()

const choices = computed(() => buildEnrollmentChoices(props.options))
const selected = ref(0)

watch(() => props.options, () => {
	selected.value = 0
})

function submit(): void {
	// A radio that was never touched keeps the first choice, which is the one the
	// Console marked as most applicable.
	const choice = choices.value[selected.value] || choices.value[0]
	if (!choice) {
		return
	}
	emit('activate', choice.mode, choice.keyID)
	emit('update:show', false)
}
</script>

<template>
	<n-modal
		:show="show"
		preset="card"
		title="选择注册密钥"
		class="etp-dialog"
		:mask-closable="false"
		@update:show="emit('update:show', $event)"
	>
		<div class="etp-stack">
			<p class="etp-paragraph">请为本机选择在工作空间「{{ workspaceName }}」中使用的注册密钥。</p>

			<n-alert v-if="options && options.current_key_unavailable" type="warning">
				当前设备密钥已不可用，请在下面选择其它密钥。
			</n-alert>

			<div>
				<label v-for="(choice, index) in choices" :key="`${choice.mode}:${choice.keyID}:${index}`" class="etp-choice">
					<n-radio
						:checked="selected === index"
						name="enrollment-choice"
						@update:checked="selected = index"
					/>
					<span>
						{{ choice.label }}
						<span class="etp-choice-hint">{{ choice.hint }}</span>
					</span>
				</label>
			</div>

			<p class="etp-paragraph etp-muted">注意：撤销共享密钥会让所有使用该密钥的设备断开连接。</p>
		</div>

		<template #footer>
			<n-space>
				<n-button @click="emit('update:show', false)">取消</n-button>
				<n-button type="primary" @click="submit">继续</n-button>
			</n-space>
		</template>
	</n-modal>
</template>
