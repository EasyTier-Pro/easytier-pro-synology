<script setup lang="ts">
import { NButton, NModal, NSpace } from 'naive-ui'
import type { Workspace } from '@/api/types'

// The Console account may reach several workspaces, so one has to be chosen
// before the device can be enrolled.

defineProps<{ show: boolean; workspaces: Workspace[] }>()

const emit = defineEmits<{
	'update:show': [value: boolean]
	select: [workspace: Workspace]
}>()

function label(workspace: Workspace): string {
	return workspace.name || workspace.slug || workspace.id || '未命名工作空间'
}
</script>

<template>
	<n-modal
		:show="show"
		preset="card"
		title="选择工作空间"
		class="etp-dialog"
		@update:show="emit('update:show', $event)"
	>
		<div class="etp-stack">
			<p class="etp-paragraph">请选择本机要加入的 Console 工作空间。</p>
			<div v-for="workspace in workspaces" :key="String(workspace.id || workspace.slug)" class="etp-row">
				<n-button @click="emit('select', workspace)">{{ label(workspace) }}</n-button>
				<span v-if="workspace.slug && workspace.name" class="etp-muted">{{ workspace.slug }}</span>
				<span v-if="workspace.role" class="etp-muted">{{ workspace.role }}</span>
			</div>
		</div>

		<template #footer>
			<n-space>
				<n-button @click="emit('update:show', false)">取消</n-button>
			</n-space>
		</template>
	</n-modal>
</template>
