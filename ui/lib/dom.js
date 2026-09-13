// Small DOM helpers shared by every page. No framework, no build step.

/**
 * h builds one element.
 * @param {string} tag
 * @param {Object} [attrs] attributes; `class`, `text`, `on*` handlers and
 *   `dataset` are supported, everything else becomes an attribute.
 * @param {Array|Node|string} [children]
 */
export function h(tag, attrs = {}, children = []) {
	const element = document.createElement(tag);
	for (const [key, value] of Object.entries(attrs || {})) {
		if (value === null || value === undefined || value === false) {
			continue;
		}
		if (key === 'class') {
			element.className = value;
		} else if (key === 'text') {
			element.textContent = value;
		} else if (key === 'dataset') {
			Object.assign(element.dataset, value);
		} else if (key.startsWith('on') && typeof value === 'function') {
			element.addEventListener(key.slice(2).toLowerCase(), value);
		} else if (value === true) {
			element.setAttribute(key, '');
		} else {
			element.setAttribute(key, value);
		}
	}
	append(element, children);
	return element;
}

/** append adds children to a node. */
export function append(node, children) {
	const list = Array.isArray(children) ? children : [ children ];
	for (const child of list) {
		if (child === null || child === undefined || child === false) {
			continue;
		}
		node.appendChild(child instanceof Node ? child : document.createTextNode(String(child)));
	}
	return node;
}

/** clear empties a node. */
export function clear(node) {
	while (node.firstChild) {
		node.removeChild(node.firstChild);
	}
	return node;
}

/** mount replaces the content of node with child (a node, array or text). */
export function mount(node, child) {
	clear(node);
	append(node, child);
	return node;
}

/** valueOrDash renders empty values as a dash. */
export function valueOrDash(value) {
	return value === null || value === undefined || value === '' ? '—' : String(value);
}

/** formatTime renders a unix timestamp in seconds. */
export function formatTime(seconds) {
	const value = Number(seconds);
	if (!value) {
		return '—';
	}
	const date = new Date(value * 1000);
	return date.toLocaleString('zh-CN', { hour12: false });
}

/** formatRemoteTime renders an RFC3339 string. */
export function formatRemoteTime(value) {
	if (!value) {
		return '—';
	}
	const parsed = new Date(value);
	if (Number.isNaN(parsed.getTime())) {
		return String(value);
	}
	return parsed.toLocaleString('zh-CN', { hour12: false });
}

/** card wraps content in a titled section. */
export function card(title, children, description) {
	return h('section', { class: 'card' }, [
		title ? h('div', { class: 'card-head' }, [
			h('h2', { text: title }),
			description ? h('p', { class: 'card-desc', text: description }) : null,
		]) : null,
		h('div', { class: 'card-body' }, children),
	]);
}

/** table builds a data table. rows are arrays of nodes or strings. */
export function table(headers, rows, options = {}) {
	return h('table', { class: 'table' }, [
		h('thead', {}, [
			h('tr', {}, headers.map((header) => h('th', { text: header }))),
		]),
		h('tbody', {}, rows.map((row) => h('tr', {}, row.map((cell) => h('td', {}, [ cell ]))))),
		options.empty && rows.length === 0
			? h('tfoot', {}, [ h('tr', {}, [ h('td', { colspan: String(headers.length), class: 'muted', text: options.empty }) ]) ])
			: null,
	]);
}

/** detailList builds a definition list of label/value rows. */
export function detailList(pairs) {
	return h('dl', { class: 'details' }, pairs.flatMap(([ label, value ]) => [
		h('dt', { text: label }),
		h('dd', {}, [ value instanceof Node ? value : valueOrDash(value) ]),
	]));
}

/** button builds a button element. */
export function button(label, options = {}) {
	return h('button', {
		class: [ 'btn', options.variant ? `btn-${options.variant}` : '', options.class || '' ].filter(Boolean).join(' '),
		type: options.type || 'button',
		disabled: options.disabled || false,
		onclick: options.onclick,
		text: label,
	});
}

/** banner renders a status strip. */
export function banner(message, level = 'info') {
	return h('div', { class: `banner banner-${level}` }, [ message ]);
}

/** spinner renders a loading placeholder. */
export function loading(message = '正在加载…') {
	return h('div', { class: 'loading' }, [ message ]);
}

/** errorBox renders an error with an optional retry button. */
export function errorBox(message, onRetry) {
	return h('div', { class: 'banner banner-error' }, [
		h('span', { text: message }),
		onRetry ? button('重试', { variant: 'secondary', onclick: onRetry }) : null,
	]);
}

/** modal renders a dialog and returns { element, close }. */
export function modal(title, body, actions = []) {
	const dialog = h('div', { class: 'modal' }, [
		h('div', { class: 'modal-panel' }, [
			h('div', { class: 'modal-head' }, [ h('h2', { text: title }) ]),
			h('div', { class: 'modal-body' }, [ body ]),
			h('div', { class: 'modal-actions' }, actions),
		]),
	]);
	const close = () => dialog.remove();
	document.body.appendChild(dialog);
	return { element: dialog, close };
}

/** notify shows a transient message. */
export function notify(message, level = 'info') {
	const host = document.getElementById('toasts');
	if (!host) {
		return;
	}
	const toast = h('div', { class: `toast toast-${level}`, text: String(message) });
	host.appendChild(toast);
	window.setTimeout(() => toast.remove(), level === 'error' ? 7000 : 4000);
}
