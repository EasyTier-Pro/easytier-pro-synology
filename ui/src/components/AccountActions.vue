<script setup lang="ts">
import { computed } from 'vue'
import { NButton, NSpace } from 'naive-ui'
import { consoleWebURL } from '@/api/client'
import type { Status } from '@/api/types'

/* 每个状态都必须给出账号操作，否则用户会被困住：例如运行时还没装好时，也必须能
   退出登录或换一个账号。 */
const props = defineProps<{ status: Status; loggedIn: boolean }>()

const emit = defineEmits<{
	logout: []
	disconnect: []
}>()

const consoleURL = computed(() => consoleWebURL(props.status.console_url))
</script>

<template>
	<!-- 账号操作对所有状态统一提供，避免用户在某个状态下无法换账号。 -->
	<section class="etp-account">
		<n-space>
			<n-button v-if="status.console_url" type="primary" tag="a" :href="consoleURL" target="_blank" rel="noopener">
				打开 Console
			</n-button>
			<n-button v-if="loggedIn" @click="emit('logout')">退出 Console 登录</n-button>
			<n-button type="error" ghost @click="emit('disconnect')">断开本机</n-button>
		</n-space>
		<p class="etp-paragraph etp-muted">
			「退出 Console 登录」只清除登录状态，不影响正在运行的连接；「断开本机」会停止服务并删除本机的设备令牌。
		</p>
		<p v-if="!status.token_present" class="etp-paragraph etp-muted">
			本机当前还没有设备令牌，断开后可以换一个账号或工作空间重新设置。
		</p>
	</section>
</template>

<style scoped>
.etp-account {
	display: flex;
	flex-direction: column;
	gap: 10px;
}
</style>
