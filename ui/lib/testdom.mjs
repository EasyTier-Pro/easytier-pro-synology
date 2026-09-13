// A minimal DOM and fetch harness for the interface tests.
//
// The pages are plain ES modules that build DOM nodes and talk to the local API
// over fetch, so both can be substituted to exercise the real rendering code
// without a browser.

class FakeNode {
	constructor(name) {
		this.nodeName = name;
		this.children = [];
		this.attributes = {};
		this.listeners = {};
		this.textContent = '';
		this.className = '';
		this.dataset = {};
		this.style = {};
		this.parentNode = null;
	}

	appendChild(child) {
		child.parentNode = this;
		this.children.push(child);
		return child;
	}

	removeChild(child) {
		const index = this.children.indexOf(child);
		if (index >= 0) {
			this.children.splice(index, 1);
		}
		return child;
	}

	replaceChildren(...nodes) {
		this.children = [];
		for (const node of nodes) {
			if (node !== null && node !== undefined) {
				this.appendChild(node);
			}
		}
	}

	prepend(node) {
		this.children.unshift(node);
		node.parentNode = this;
	}

	remove() {
		if (this.parentNode) {
			this.parentNode.removeChild(this);
		}
	}

	setAttribute(key, value) {
		this.attributes[key] = String(value);
	}

	getAttribute(key) {
		return this.attributes[key];
	}

	addEventListener(type, handler) {
		(this.listeners[type] = this.listeners[type] || []).push(handler);
	}

	querySelector() {
		return null;
	}

	querySelectorAll() {
		return [];
	}

	get firstChild() {
		return this.children[0] || null;
	}

	get isConnected() {
		return true;
	}
}

class FakeText extends FakeNode {
	constructor(text) {
		super('#text');
		this.textContent = text;
	}
}

/** install puts the harness on the globals the page modules use. */
export function install({ responses = {} } = {}) {
	const calls = [];

	globalThis.Node = FakeNode;
	globalThis.document = {
		createElement: (name) => new FakeNode(name),
		createTextNode: (text) => new FakeText(text),
		contains: () => true,
		body: new FakeNode('body'),
	};
	globalThis.window = {
		setTimeout: (fn, ms) => setTimeout(fn, ms),
		clearTimeout: (id) => clearTimeout(id),
		location: { origin: 'https://nas:5001', href: 'https://nas:5001/3rdparty/easytier-pro/index.html' },
		open: () => null,
		addEventListener: () => {},
	};
	// navigator is a read-only global in Node, so it is defined rather than set.
	Object.defineProperty(globalThis, 'navigator', {
		value: { clipboard: { writeText: () => Promise.resolve() } },
		configurable: true,
	});
	globalThis.fetch = async (path) => {
		calls.push(String(path));
		const body = responses[String(path)];
		if (body === undefined) {
			return { status: 404, json: async () => ({ ok: false, error: 'method_not_found' }) };
		}
		return { status: 200, json: async () => body };
	};
	globalThis.__apiCalls = calls;
	return calls;
}

/** texts collects every text node under a node, in order. */
export function texts(node, out = []) {
	if (!node) {
		return out;
	}
	if (node.nodeName === '#text') {
		out.push(node.textContent);
	} else if (node.textContent && node.children.length === 0) {
		out.push(node.textContent);
	}
	for (const child of node.children || []) {
		texts(child, out);
	}
	return out;
}

/** findByText returns the first node whose own text equals value. */
export function findByText(node, value) {
	if (!node) {
		return null;
	}
	if (node.textContent === value && node.children.length === 0) {
		return node;
	}
	for (const child of node.children || []) {
		const found = findByText(child, value);
		if (found) {
			return found;
		}
	}
	return null;
}
