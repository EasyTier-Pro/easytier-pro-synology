// Every state of the overview has to offer a way back to the start. A state
// without account actions traps the user: the runtime-setup step once had only
// "install", so a device that had enrolled but not installed could neither log
// out nor change workspace.
import test from 'node:test';
import assert from 'node:assert/strict';
import { install, findByText, texts } from '../lib/testdom.mjs';

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

// The no-TUN mode keeps the node in the network: it still has a virtual IP, it
// can be reached by peers, and it still routes subnets and relays. Only the
// host itself loses the virtual interface. The interface once described it as
// "no virtual IP, relay only", which understated what the mode can do.
test('no-TUN notice describes reachability, not isolation', async () => {
	// The runtime is installed but the service is stopped, so the notice about
	// the missing virtual interface is shown.
	const root = await renderWith({
		'api/status': { ...connected, core_installed: true, cli_installed: true, tun_capable: false },
	});
	const notice = texts(root).join('\n');
	assert.match(notice, /无 TUN 模式/);
	assert.match(notice, /其它节点可以主动访问本机/, 'the notice must say the node stays reachable');
	assert.match(notice, /子网路由和中继/, 'the notice must say routing and relay still work');
	assert.doesNotMatch(notice, /没有自己的虚拟 IP/, 'the node does have a virtual IP');
	assert.doesNotMatch(notice, /没有虚拟 IP/, 'the node does have a virtual IP');
});

// Losing CAP_NET_RAW makes the node unreachable until the Console is told. The
// interface may only claim that was done when it actually was: writing the
// setting needs a Console session.
test('unsynced bind capability asks for a Console login', async () => {
	const root = await renderWith({
		'api/status': {
			...connected, core_installed: true, cli_installed: true,
			tun_capable: false, bind_capable: false, mode_synced: false,
		},
	});
	const notice = texts(root).join('\n');
	assert.match(notice, /无法连接网络中的其它设备/, 'the symptom must be named');
	assert.match(notice, /登录 EasyTier Console/, 'the required action must be named');
});

// The permission itself is an implementation detail of the package: a NAS user
// can neither understand it nor act on it, so it must never appear.
test('the capability name never reaches the user', async () => {
	for (const modeSynced of [true, false]) {
		const root = await renderWith({
			'api/status': {
				...connected, core_installed: true, cli_installed: true,
				tun_capable: false, bind_capable: false, mode_synced: modeSynced,
			},
		});
		const notice = texts(root).join('\n');
		assert.doesNotMatch(notice, /CAP_NET_RAW/, 'no capability name may be shown');
		assert.doesNotMatch(notice, /不绑定网卡/, 'no internal mode name may be shown');
		assert.doesNotMatch(notice, /绑定到网卡/, 'no internal mechanism may be shown');
		assert.doesNotMatch(notice, /setcap cap_net_raw/, 'no shell command for this case');
	}
});

// Once the setting is in place the user has nothing to do and nothing to know.
test('synced bind capability shows no notice at all', async () => {
	const root = await renderWith({
		'api/status': {
			...connected, core_installed: true, cli_installed: true,
			tun_capable: false, bind_capable: false, mode_synced: true,
		},
	});
	const notice = texts(root).join('\n');
	assert.doesNotMatch(notice, /不绑定网卡/);
	assert.doesNotMatch(notice, /登录 EasyTier Console/);
	assert.doesNotMatch(notice, /CAP_NET_RAW/);
});

test('no warning once the capability is present', async () => {
	const root = await renderWith({
		'api/status': { ...connected, core_installed: true, cli_installed: true, tun_capable: false, bind_capable: true },
	});
	assert.doesNotMatch(texts(root).join('\n'), /CAP_NET_RAW/);
});
