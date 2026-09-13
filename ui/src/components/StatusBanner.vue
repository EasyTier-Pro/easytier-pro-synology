<script setup lang="ts">
import { NAlert } from 'naive-ui'

// A single status strip. The pages use these for conditions that concern the
// whole section, so the wrapper keeps their wording and severity in one place.
withDefaults(defineProps<{
	type?: 'info' | 'success' | 'warning' | 'error'
	title?: string
}>(), {
	type: 'info',
})
</script>

<template>
	<n-alert :type="type" :title="title" :show-icon="type !== 'info'" class="etp-banner">
		<slot />
	</n-alert>
</template>

<style scoped>
.etp-banner {
	border-radius: 8px;
}
</style>
