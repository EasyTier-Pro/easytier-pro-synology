// Every state of the overview has to offer a way back to the start. A state
// without account actions traps the user: the runtime-setup step once had only
// "install", so a device that had enrolled but not installed could neither log
// out nor change workspace.
import test from 'node:test';
import assert from 'node:assert/strict';
import { install, findByText } from '../lib/testdom.mjs';

install();

const { render, unmount } = await import('./overview.js');

const connected = {
	ok: true,
	enabled: true,
	running: false,
	token_present: true,
	core_installed: false,
	cli_installed: false,
	console_url: 'https://api.console.easytier.net',
	install_dir: '/volume1/@appdata/easytier-pro/runtime',
	tun_capable: false,
	workspace_id: 'd04e97ef-9d62-40a1-b2c5-168b04c30494',
};

function apiStub(overrides = {}) {
	const responses = {
		'api/status': connected,
		'api/auth/status': { ok: true, logged_in: true },
		'api/runtime/download/status': { state: 'idle', phase: '', percent: 0 },
		...overrides,
	};
	install({ responses });
}

async function renderWith(overrides) {
	apiStub(overrides);
	const root = await render();
	// The page renders asynchronously after mount.
	await new Promise((resolve) => setTimeout(resolve, 20));
	return root;
}

test.afterEach(() => unmount());

test('runtime setup offers a way back to the start', async () => {
	const root = await renderWith();
	assert.ok(findByText(root, '安装 EasyTier 运行时'), 'expected the runtime setup step');
	assert.ok(findByText(root, '断开本机'), 'no way to reset the device from the runtime setup step');
	assert.ok(findByText(root, '退出 Console 登录'), 'no way to log out from the runtime setup step');
});

test('runtime setup keeps its install action', async () => {
	const root = await renderWith();
	assert.ok(findByText(root, '安装并继续'), 'expected the install action');
});

test('first use offers setup instead of account actions', async () => {
	const root = await renderWith({
		'api/status': { ...connected, token_present: false },
		'api/auth/status': { ok: true, logged_in: false },
	});
	assert.ok(findByText(root, '首次使用'), 'expected the first-use step');
	assert.equal(findByText(root, '断开本机'), null, 'nothing to disconnect yet');
});

test('workspace setup offers logout', async () => {
	const root = await renderWith({
		'api/status': { ...connected, token_present: false },
	});
	assert.ok(findByText(root, '完成本机设置'), 'expected the workspace step');
	assert.ok(findByText(root, '退出 Console 登录'), 'no way to log out from the workspace step');
});
