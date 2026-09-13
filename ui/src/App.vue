<script setup lang="ts">
import { NConfigProvider, NLayout, darkTheme, zhCN, dateZhCN } from 'naive-ui'
import { computed } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import { routes } from './router'
import { isDark } from './naive'

const route = useRoute()
const theme = computed(() => (isDark.value ? darkTheme : null))

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
			</header>
			<main class="etp-main">
				<RouterView />
			</main>
		</n-layout>
	</n-config-provider>
</template>
