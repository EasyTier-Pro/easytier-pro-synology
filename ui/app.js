// Application shell: hash routing and page mounting. Pages are plain ES
// modules that export render() and optional unmount().
import { h, mount, loading, errorBox } from './lib/dom.js';

const routes = [
	{ id: 'overview', label: '概览', load: () => import('./pages/overview.js') },
	{ id: 'networks', label: '网络', load: () => import('./pages/networks.js') },
	{ id: 'settings', label: '设置', load: () => import('./pages/settings.js') },
	{ id: 'logs', label: '日志', load: () => import('./pages/logs.js') },
];

let activePage = null;

function currentRouteID() {
	const raw = window.location.hash.replace(/^#\/?/, '');
	const id = raw.split(/[/?]/)[0];
	return routes.some((route) => route.id === id) ? id : 'overview';
}

/** navigate switches to one page. */
export function navigate(id) {
	if (currentRouteID() === id) {
		renderRoute();
		return;
	}
	window.location.hash = `#/${id}`;
}

function renderNav(activeID) {
	const nav = document.getElementById('nav');
	mount(nav, routes.map((route) => h('a', {
		class: `tab${route.id === activeID ? ' tab-active' : ''}`,
		href: `#/${route.id}`,
		text: route.label,
	})));
}

function messageOf(error) {
	return error && error.message ? error.message : String(error);
}

async function renderRoute() {
	const view = document.getElementById('view');
	if (!view) {
		return;
	}
	const activeID = currentRouteID();
	const route = routes.find((entry) => entry.id === activeID) || routes[0];
	if (activePage && typeof activePage.unmount === 'function') {
		try {
			activePage.unmount();
		} catch (error) {
			// A page that fails to clean up must not block navigation.
		}
	}
	activePage = null;
	renderNav(route.id);
	mount(view, loading());
	try {
		const module = await route.load();
		activePage = module;
		mount(view, await module.render());
	} catch (error) {
		mount(view, errorBox(messageOf(error), renderRoute));
	}
}

/** refresh re-renders the current page. */
export function refresh() {
	return renderRoute();
}

window.addEventListener('hashchange', renderRoute);
renderRoute();
