<script setup lang="ts">
import { NButton, NConfigProvider, NLayout, NSelect, darkTheme, zhCN, dateZhCN } from 'naive-ui'
import { computed, onMounted } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { routes } from './router'
import { isDark, setThemeMode, themeMode, type ThemeMode } from './theme'
import { consoleWebURL } from './api/client'
import { refreshStatus, shared } from './stores/resources'

const route = useRoute()
const theme = computed(() => (isDark.value ? darkTheme : null))

// The appearance control: following the system stays the default, and choosing
// one is remembered for the next visit.
const themeOptions: Array<{ label: string; value: ThemeMode }> = [
	{ label: '跟随系统', value: 'system' },
	{ label: '浅色', value: 'light' },
	{ label: '深色', value: 'dark' },
]

// The Console is the other half of this product - networks are created and
// managed there - so it is reachable from every page rather than from the bottom
// of one card. It links to the device list, which is where this machine appears.
// Until the address is known there is nothing to point at, so the button waits.
const consoleLink = computed(() => {
	const address = shared().status.data.value?.console_url
	return address ? consoleWebURL(address, '/devices') : ''
})

// The shell owns this because the header needs the device address on every page,
// including the ones that have no use for the device state themselves. A page
// that already started reading it wins - the check makes the two agree on one
// read rather than issuing a second.
onMounted(() => {
	const status = shared().status
	if (!status.settled.value && !status.loading.value) {
		void refreshStatus()
	}
})

// The navigation entries come from the route table so a new page only has to be
// declared once.
const navItems = routes
	.filter((entry) => entry.meta && entry.meta.label)
	.map((entry) => ({ name: String(entry.name), label: String(entry.meta!.label) }))
</script>

<template>
	<n-config-provider :theme="theme" :locale="zhCN" :date-locale="dateZhCN">
		<n-layout class="etp-shell">
			<header class="etp-header">
				<div class="etp-brand">
					<span class="etp-brand-mark">ET</span>
					<div class="etp-brand-text">
						<h1>EasyTier Pro</h1>
						<p>把本机接入 EasyTier Console 的虚拟网络</p>
					</div>
				</div>
				<nav class="etp-nav" aria-label="页面">
					<RouterLink
						v-for="item in navItems"
						:key="item.name"
						:to="{ name: item.name }"
						class="etp-nav-link"
						:class="{ 'is-active': route.name === item.name }"
					>{{ item.label }}</RouterLink>
				</nav>
				<div class="etp-actions">
					<n-button
						v-if="consoleLink"
						size="small"
						type="primary"
						tag="a"
						:href="consoleLink"
						target="_blank"
						rel="noopener"
					>打开 Console</n-button>
					<n-select
						:value="themeMode"
						:options="themeOptions"
						size="small"
						class="etp-theme-select"
						aria-label="界面配色"
						@update:value="setThemeMode"
					/>
				</div>
			</header>
			<main class="etp-main">
				<RouterView />
			</main>
		</n-layout>
	</n-config-provider>
</template>
