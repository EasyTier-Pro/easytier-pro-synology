<script setup lang="ts">
import { computed } from 'vue'
import StatusBanner from './StatusBanner.vue'
import type { Status } from '@/api/types'

/* 缺少 CAP_NET_RAW 时节点能注册但连不上任何 peer。这个设置由守护进程自动写到 Console
   上（节点的 bind_device override），正常工作时用户完全不需要知道它的存在，因此不显示
   任何提示。只有同步还没完成、peer 确实连不上时，才提示用户需要先登录 Console。

   提示里不提权限名：这是套件内部的实现细节，用户既看不懂也无法处理。 */
const props = defineProps<{ status: Status }>()

const visible = computed(() => Boolean(
	props.status.core_installed
	&& !props.status.bind_capable
	&& !props.status.mode_synced,
))
</script>

<template>
	<StatusBanner v-if="visible" type="error">
		<p class="etp-paragraph">本机尚未完成网络设置同步，目前无法连接网络中的其它设备。</p>
		<p class="etp-paragraph">请先登录 EasyTier Console，本机会自动完成设置。</p>
	</StatusBanner>
</template>
