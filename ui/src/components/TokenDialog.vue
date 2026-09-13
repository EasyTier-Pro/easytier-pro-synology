<script setup lang="ts">
import { ref, watch } from 'vue'
import { NButton, NForm, NFormItem, NInput, NModal, NSpace } from 'naive-ui'

// A device enrolment token as an alternative to the account login, for an
// operator who already has one.

const props = defineProps<{ show: boolean }>()

const emit = defineEmits<{
	'update:show': [value: boolean]
	submit: [token: string, configServer: string]
	invalid: []
}>()

const token = ref('')
const configServer = ref('')

watch(() => props.show, (visible) => {
	if (visible) {
		token.value = ''
		configServer.value = ''
	}
})

function submit(): void {
	const trimmed = token.value.trim()
	if (!trimmed) {
		emit('invalid')
		return
	}
	emit('submit', trimmed, configServer.value.trim())
	emit('update:show', false)
}
</script>

<template>
	<n-modal
		:show="show"
		preset="card"
		title="使用设备令牌"
		class="etp-dialog"
		@update:show="emit('update:show', $event)"
	>
		<div class="etp-stack">
			<p class="etp-paragraph">
				粘贴从 EasyTier Console 获取的设备注册令牌。令牌只保存在本机，仅 root 可读。
			</p>
			<n-form label-placement="top">
				<n-form-item label="设备令牌">
					<n-input
						v-model:value="token"
						type="password"
						show-password-on="click"
						:input-props="{ autocomplete: 'off' }"
					/>
				</n-form-item>
				<n-form-item label="配置服务器（可选）">
					<n-input v-model:value="configServer" placeholder="可留空，将自动从 Console 读取" />
					<template #feedback>
						<span class="etp-muted">
							例如 tcp://et-web.console.easytier.net:22020；留空时自动获取。
						</span>
					</template>
				</n-form-item>
			</n-form>
		</div>

		<template #footer>
			<n-space>
				<n-button @click="emit('update:show', false)">取消</n-button>
				<n-button type="primary" @click="submit">连接</n-button>
			</n-space>
		</template>
	</n-modal>
</template>
