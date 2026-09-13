import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'
import OverviewView from './views/OverviewView.vue'
import NetworksView from './views/NetworksView.vue'
import SettingsView from './views/SettingsView.vue'
import LogsView from './views/LogsView.vue'

// Hash routing: the interface is served from /3rdparty/easytier-pro/ and the DSM
// application window keeps its own path, so only the fragment identifies a page.
// The ids match the previous interface, so existing links keep working.
//
// The views are imported eagerly on purpose. There are four of them, they ship
// in one bundle either way, and a loaded module means switching pages never
// blanks the content area waiting for a chunk.
export const routes: RouteRecordRaw[] = [
	{ path: '/overview', name: 'overview', component: OverviewView, meta: { label: '概览' } },
	{ path: '/networks', name: 'networks', component: NetworksView, meta: { label: '网络' } },
	{ path: '/settings', name: 'settings', component: SettingsView, meta: { label: '设置' } },
	{ path: '/logs', name: 'logs', component: LogsView, meta: { label: '日志' } },
	{ path: '/:pathMatch(.*)*', redirect: '/overview' },
]

export const router = createRouter({
	history: createWebHashHistory(),
	routes,
})
