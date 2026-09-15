<script setup lang="ts">
import { computed } from 'vue'
import { NCode, NCollapse, NCollapseItem, NButton } from 'naive-ui'
import StatusBanner from './StatusBanner.vue'
import { copyText } from '@/composables/useClipboard'
import { platform } from '@/platform'
import type { Status } from '@/api/types'

/* 缺少 CAP_NET_ADMIN 时本机只能中继运行：给出管理员一次性授权命令。 */
const props = defineProps<{ status: Status }>()

// Only DSM keeps the package away from root; other platforms grant TUN directly.
const visible = computed(() =>
	platform.showTunNotice && Boolean(props.status.core_installed && !props.status.tun_capable))

const runtimeDir = computed(() => props.status.install_dir || '/volume1/@appdata/easytier-pro/runtime')

const command = computed(() => `sudo setcap cap_net_admin,cap_net_raw+ep ${runtimeDir.value}/easytier-core`)
</script>

<template>
	<StatusBanner v-if="visible" type="warning">
		<p class="etp-paragraph">
			本机当前以「无 TUN 模式」运行：DSM 不允许套件以 root 运行，套件因此拿不到创建虚拟网卡的权限。
		</p>
		<p class="etp-paragraph">
			这不会让本机退出网络：本机仍会获得虚拟 IP，其它节点可以主动访问本机（也能经本机虚拟 IP
			访问本机上的服务），并且照常可以做子网路由和中继。
			<br>
			唯一的区别是本机自身没有虚拟网卡，因此 NAS 上的程序无法主动访问网络里的其它节点。
		</p>
		<p v-if="status.mode_synced" class="etp-paragraph">
			已自动在 EasyTier Console 上把本机节点设为「无 TUN 模式」，这样下发的配置才与本机权限一致。
			<br>
			授予权限后本机会自动取消该设置，恢复完整模式。
		</p>
		<p v-else class="etp-paragraph">
			把该设置写入 EasyTier Console 需要 Console 登录；请先登录，本机会自动完成设置。
		</p>
		<n-collapse class="etp-tun-details">
			<n-collapse-item title="想让 NAS 本身拥有虚拟网卡（可选）" name="grant">
				<p class="etp-paragraph etp-muted">
					默认不需要这样做。只有当你希望 NAS 上的程序能主动访问其它节点时，才需要由管理员授权
					（SSH，或用「控制面板 → 任务计划」新建以 root 运行的脚本任务），然后重新启动套件：
				</p>
				<div class="etp-command">
					<n-code :code="command" language="bash" word-wrap />
					<n-button size="small" @click="copyText(command)">复制命令</n-button>
				</div>
				<p class="etp-paragraph etp-muted">
					每次重新下载运行时后，新文件都会丢失该权限，本页会再次显示这条提示。
				</p>
			</n-collapse-item>
		</n-collapse>
	</StatusBanner>
</template>

<style scoped>
.etp-tun-details {
	margin-top: 10px;
}
</style>
