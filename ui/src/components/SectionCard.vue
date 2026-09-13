<script setup lang="ts">
import { NCard } from 'naive-ui'

// One titled section of a page. It mirrors the daemon's card layout so every
// page reads the same, and it renders no header at all when there is no title,
// which the actions section relies on.
defineProps<{
	title?: string
	description?: string
	/** Dims the body while a reload of this section is in flight. */
	loading?: boolean
}>()
</script>

<template>
	<n-card :title="title" size="small" class="etp-section" :class="{ 'is-loading': loading }">
		<template v-if="title && description" #header-extra>
			<span class="etp-section-desc etp-muted">{{ description }}</span>
		</template>
		<p v-if="!title && description" class="etp-paragraph etp-muted">{{ description }}</p>
		<div class="etp-stack">
			<slot />
		</div>
	</n-card>
</template>

<style scoped>
.etp-section {
	border-radius: 10px;
}

.etp-section-desc {
	font-size: 12px;
	font-weight: 400;
}

/* A reloading section stays legible: it is dimmed rather than emptied. */
.etp-section.is-loading {
	opacity: 0.72;
	transition: opacity 0.15s ease;
}
</style>
