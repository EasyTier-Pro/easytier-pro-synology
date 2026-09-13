<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { NButton, NCheckbox, NForm, NFormItem, NInput, NSpace, NSpin } from 'naive-ui'
import { api } from '@/api/client'
import { messageOf, needsRelogin } from '@/api/errors'
import { notify } from '@/naive'
import { shared } from '@/stores/resources'
import SectionCard from '@/components/SectionCard.vue'
import StatusBanner from '@/components/StatusBanner.vue'
import type { Settings } from '@/api/types'

// The everyday options plus the advanced Console endpoints. The form reads the
// daemon's stored settings and writes them back whole.

interface FormState {
	enabled: boolean
	consoleURL: string
	insecure: boolean
	configServer: string
}

const { status } = shared()
const form = ref<FormState>({ enabled: false, consoleURL: '', insecure: false, configServer: '' })
const saving = ref(false)
const bannerText = ref<string | null>(null)
const bannerType = ref<'error' | 'warning'>('error')

const installDir = computed(() => status.data.value?.install_dir || '—')

/** validateConsoleURL mirrors the daemon's own check, so mistakes are caught here. */
function validateConsoleURL(value: string): string {
	if (!value) {
		return '请输入 Console 地址。'
	}
	if (/\s/.test(value)) {
		return 'Console 地址不能包含空格，请输入完整的 http:// 或 https:// 地址。'
	}
	let parsed: URL
	try {
		parsed = new URL(value)
	} catch {
		return 'Console 地址格式不正确，请输入完整的 http:// 或 https:// 地址，例如 https://api.console.easytier.net。'
	}
	if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
		return 'Console 地址必须以 http:// 或 https:// 开头。'
	}
	if (!parsed.hostname) {
		return 'Console 地址缺少服务器名称，请输入完整的 http:// 或 https:// 地址。'
	}
	return ''
}

/** validateConfigServer accepts an empty value: it is filled in after connecting. */
function validateConfigServer(value: string): string {
	if (!value) {
		return ''
	}
	if (/\s/.test(value)) {
		return '配置服务器地址不能包含空格。'
	}
	if (!/^[A-Za-z][A-Za-z0-9+.-]*:\/\/\S+$/.test(value)) {
		return '配置服务器地址需要以「协议://服务器地址」的格式填写，例如 tcp://example.com:22020；不确定时请留空，连接成功后会自动填入。'
	}
	return ''
}

/** fill copies the stored settings into the form. */
function fill(): void {
	const value = status.data.value
	if (!value) {
		return
	}
	form.value = {
		enabled: value.enabled === true,
		consoleURL: value.console_url || '',
		insecure: value.allow_insecure_console === true,
		configServer: value.config_server || '',
	}
}

// The form mirrors what the daemon reports. Nothing else reloads this page's
// status while the operator is editing, so there is no edit to protect: the only
// reload is the one after a save, whose purpose is to show the stored values.
watch(() => status.data.value, fill, { immediate: true })

async function save(): Promise<void> {
	if (saving.value) {
		return
	}
	const payload: Settings = {
		enabled: form.value.enabled,
		console_url: form.value.consoleURL.trim(),
		allow_insecure_console: form.value.insecure,
		config_server: form.value.configServer.trim(),
	}
	const failure = validateConsoleURL(payload.console_url) || validateConfigServer(payload.config_server)
	if (failure) {
		bannerType.value = 'error'
		bannerText.value = failure
		return
	}

	saving.value = true
	bannerText.value = null
	try {
		await api.applySettings(payload)
		notify('设置已保存。', 'success')
		// The daemon normalises what it stores, so the form is refilled from it.
		await status.reload()
		fill()
	} catch (error) {
		bannerType.value = needsRelogin(error) ? 'warning' : 'error'
		bannerText.value = needsRelogin(error)
			? 'DSM 登录已失效，请重新登录 DSM 后再保存设置。'
			: messageOf(error) || '操作失败，请稍后重试。'
	} finally {
		saving.value = false
	}
}

onMounted(() => {
	void status.reload()
})
</script>

<template>
	<div class="etp-stack">
		<div v-if="bannerText">
			<StatusBanner :type="bannerType">{{ bannerText }}</StatusBanner>
		</div>

		<div v-if="status.loading.value && !status.settled.value" class="etp-loading">
			<n-spin size="small" />
			<span class="etp-muted">正在读取本机设置…</span>
		</div>

		<template v-else>
			<SectionCard title="日常" description="日常使用只需要保留默认设置。">
				<n-checkbox v-model:checked="form.enabled">开机自动连接</n-checkbox>
				<p class="etp-paragraph etp-muted">群晖开机后自动重新接入 EasyTier 虚拟网络。</p>
			</SectionCard>

			<SectionCard title="高级" description="除非清楚自己在做什么，否则请保持默认。">
				<n-form label-placement="top">
					<n-form-item label="Console 地址">
						<n-input v-model:value="form.consoleURL" placeholder="https://api.console.easytier.net" />
						<template #feedback>
							<span class="etp-muted">EasyTier Console 的服务地址，一般以 https:// 开头。</span>
						</template>
					</n-form-item>

					<n-form-item label="允许使用不安全的 HTTP 地址">
						<n-checkbox v-model:checked="form.insecure" />
						<template #feedback>
							<span class="etp-muted">
								仅在 Console 地址为 http:// 时需要开启；自己搭建 Console 时才可能用到，正常情况下请保持关闭。
							</span>
						</template>
					</n-form-item>

					<n-form-item label="配置服务器地址">
						<n-input v-model:value="form.configServer" placeholder="tcp://example.com:22020" />
						<template #feedback>
							<span class="etp-muted">
								留空即可：成功连接后会自动填入；需要手动填写时请使用「协议://服务器地址」的格式，例如 tcp://example.com:22020。
							</span>
						</template>
					</n-form-item>

					<n-form-item label="运行时安装目录">
						<span class="etp-mono">{{ installDir }}</span>
						<template #feedback>
							<span class="etp-muted">运行时文件的安装目录由 DSM 统一管理，无需手动修改。</span>
						</template>
					</n-form-item>
				</n-form>
			</SectionCard>

			<SectionCard>
				<n-space>
					<n-button type="primary" :loading="saving" :disabled="saving" @click="save">保存</n-button>
				</n-space>
				<p class="etp-paragraph etp-muted">
					虚拟网卡对局域网的访问请在 DSM 控制面板 → 安全性 → 防火墙 中放行。
				</p>
			</SectionCard>
		</template>
	</div>
</template>

<style scoped>
.etp-loading {
	display: flex;
	align-items: center;
	gap: 10px;
	padding: 12px 0;
}
</style>
