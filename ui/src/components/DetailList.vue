<script setup lang="ts">
import { NDescriptions, NDescriptionsItem } from 'naive-ui'
import { valueOrDash } from '@/utils/format'

// A label/value list. Values that are empty render as a dash, and a slot lets a
// caller supply richer content (a monospace address, an action) for one row.
export interface DetailEntry {
	label: string
	value?: unknown
	mono?: boolean
}

defineProps<{ entries: DetailEntry[] }>()
</script>

<template>
	<n-descriptions :column="1" label-placement="left" size="small" bordered class="etp-details">
		<n-descriptions-item v-for="entry in entries" :key="entry.label" :label="entry.label">
			<slot :name="entry.label" :entry="entry">
				<span :class="{ 'etp-mono': entry.mono }">{{ valueOrDash(entry.value) }}</span>
			</slot>
		</n-descriptions-item>
	</n-descriptions>
</template>

<style scoped>
.etp-details :deep(.n-descriptions-table-content) {
	font-size: 13px;
}
</style>
