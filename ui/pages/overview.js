// 概览页：驱动「不可用 → 首次使用 → 工作空间选择 → 运行时就绪 → 服务停止 → 运行中」状态机。
import {
	h, card, table, detailList, button, banner, modal, notify, loading, valueOrDash,
} from '../lib/dom.js';
import { api, needsRelogin, consoleWebURL } from '../lib/api.js';
import { createTimerScope } from '../lib/timers.js';

// Poll timers are owned by a scope so that leaving the page stops them, while
// coming back to the page can start polling again. The overview polls during
// several steps (device login, connection changes, runtime install), so a
// one-way stop would leave a later visit unable to report progress at all.
const timerScope = createTimerScope();
const { after } = timerScope;

/** 以给定初始延迟轮询一个会异步完成的本机操作，直到完成或失败。 */
function pollOperation(operationID, handlers, initialDelay = 1200) {
	let failures = 0;
	const tick = () => {
		api.operationStatus(operationID).then((operation) => {
			failures = 0;
			if (operation.state === 'queued' || operation.state === 'running') {
				if (handlers.progress) {
					handlers.progress(operation);
				}
				after(1200, tick);
				return;
			}
			if (operation.state === 'failed') {
				handlers.failed(operation);
				return;
			}
			if (operation.state === 'completed') {
				handlers.completed(operation);
				return;
			}
			handlers.failed({ message: '操作返回了未知状态。' });
		}).catch((error) => {
			failures += 1;
			if (handlers.error) {
				handlers.error(error, failures);
			}
			if (failures < 6) {
				after(2000, tick);
			} else {
				handlers.failed({ message: `无法查询操作进度：${messageOf(error)}` });
			}
		});
	};
	after(initialDelay, tick);
}

export function unmount() {
	timerScope.invalidate();
}

function messageOf(error) {
	return error && error.message ? error.message : String(error);
}

function networkName(network) {
	return network && (network.name || network.network_name || network.id) || '未命名网络';
}

function enrollmentKeyName(key) {
	const name = key && (key.display_name || key.key_code || key.id);
	if (key && key.key_code && key.key_code !== name) {
		return `${name} · ${key.key_code}`;
	}
	return name || '未命名密钥';
}

/** peers 可能是数组、{result:[...]} 或按条目嵌套的对象。 */
function peerEntries(value) {
	if (!value) {
		return [];
	}
	if (Array.isArray(value)) {
		return value.flatMap((item) => (item && Array.isArray(item.result) ? item.result : [ item ]));
	}
	if (Array.isArray(value.result)) {
		return value.result;
	}
	if (typeof value === 'object') {
		return Object.values(value);
	}
	return [];
}

function peerCount(value) {
	return peerEntries(value).length;
}

/** 本机虚拟 IP：先看 node 顶层/结果数组，再从 Peer 里找本机节点。 */
function localIPv4(summary) {
	const match = (entry) => (entry && typeof entry === 'object'
		? entry.ipv4_addr || entry.ipv4Addr || null
		: null);
	const node = summary && summary.node;
	if (node && typeof node === 'object') {
		const direct = match(node);
		if (direct) {
			return direct;
		}
		const items = Array.isArray(node) ? node
			: (Array.isArray(node.result) ? node.result : []);
		for (const item of items) {
			// Multi-instance output wraps each instance in a result object.
			const found = match(item) || match(item && item.result);
			if (found) {
				return found;
			}
		}
	}
	for (const peer of peerEntries(summary && summary.peers)) {
		const found = match(peer);
		if (found) {
			return found;
		}
	}
	return '';
}

// 下载阶段文案，与后端阶段名一一对应。
const downloadPhases = {
	queued: '正在等待开始',
	release: '正在读取稳定版本信息',
	metadata: '正在核对版本信息',
	architecture: '正在确认处理器架构',
	storage: '正在检查可用存储空间',
	download: '正在下载运行时',
	verify: '正在校验下载内容',
	extract: '正在解压运行时',
	validate: '正在验证运行时',
	install: '正在安装运行时',
	restart: '正在重启连接服务',
	done: '运行时安装完成',
	interrupted: '上次安装被中断',
};

function downloadMessage(download) {
	const message = downloadPhases[download.phase] || (download.state === 'running' ? '正在准备…' : '');
	if (download.state === 'failed') {
		return download.message || (message ? `安装未完成：${message}` : '安装未完成。');
	}
	return message;
}

// 后台操作阶段文案。
const operationPhases = {
	queued: '正在排队执行本机设置变更…',
	console: '正在核对 Console 账号与注册密钥…',
	release: '正在读取 EasyTier 连接配置…',
	service: '正在启动服务并检查连接…',
	network: '正在向 Console 更新网络成员…',
	complete: '本机设置已完成。',
};

function operationMessage(operation) {
	return operationPhases[operation && operation.phase] || '正在执行本机设置变更…';
}

/** 后台操作进行中时展示的对话框。 */
function showOperationProgress(title, operationID, outcome) {
	const text = h('p', { class: 'muted', text: '正在排队执行本机设置变更…' });
	const dialog = h('div', {}, [
		h('p', { text: '请稍候，正在完成本机设置变更。' }),
		text,
		h('p', { class: 'muted', text: '操作在后台继续执行，可以暂时离开本页面。' }),
	]);
	const view = modalDialog(title, dialog);
	pollOperation(operationID, {
		progress: (operation) => {
			text.textContent = operationMessage(operation);
		},
		completed: (operation) => {
			view.close();
			outcome.completed(operation);
		},
		failed: (operation) => {
			view.close();
			notify(operation.message || '操作失败，请重试。', 'error');
			if (outcome.failed) {
				outcome.failed(operation);
			}
		},
		error: () => {},
	});
	return view;
}

/** modalDialog 直接透传 dom.js 的 modal，返回 { element, close }。 */
function modalDialog(title, body, actions = []) {
	return modal(title, body, actions);
}

export async function render() {
	const root = h('div', { class: 'page' });

	const reset = () => {
		unmount();
		mountRoot();
	};
	const mountRoot = () => {
		root.replaceChildren(loading());
		renderInto(root).catch(() => {});
	};

	async function renderInto(host) {
		let status;
		let auth;
		let download = { state: 'idle', phase: '', percent: 0 };
		try {
			[ status, auth, download ] = await Promise.all([
				api.status(),
				api.authStatus(),
				api.downloadStatus().catch(() => ({ state: 'idle', phase: '', percent: 0 })),
			]);
		} catch (error) {
			host.replaceChildren(renderUnavailable(error, reset));
			return;
		}

		const loggedIn = Boolean(auth.logged_in);
		if (!loggedIn && !status.token_present) {
			host.replaceChildren(renderFirstUse({ reset }));
			return;
		}
		if (!status.token_present) {
			// A device token alone is a complete setup: only users who have
			// not connected yet need to choose a workspace.
			host.replaceChildren(renderWorkspaceSetup({ reset, loggedIn }));
			return;
		}
		if (!status.core_installed || !status.cli_installed) {
			host.replaceChildren(renderRuntimeSetup(status, download, { reset, loggedIn }));
			if (download.state === 'queued' || download.state === 'running') {
				pollDownload(host, status, { reset, loggedIn });
			}
			return;
		}
		if (!status.running) {
			host.replaceChildren(renderServiceStopped(status, { reset, loggedIn }));
			return;
		}
		host.replaceChildren(loading('正在读取本机连接状态…'));
		let summary = null;
		let networks = null;
		const [ summaryResult, networksResult ] = await Promise.allSettled([
			api.localSummary(),
			api.networks(),
		]);
		if (summaryResult.status === 'fulfilled') {
			summary = summaryResult.value;
		}
		if (networksResult.status === 'fulfilled') {
			networks = networksResult.value;
		}
		if (!host.isConnected && !document.contains(host)) {
			return;
		}
		host.replaceChildren(renderRunning(status, auth, summary, networks, { reset, loggedIn }));
	}

	mountRoot();
	return root;
}

/* 1. 不可用：状态读取失败。 */
function renderUnavailable(error, reset) {
	if (needsRelogin(error)) {
		return card('需要有效的 DSM 登录会话', [
			banner(h('div', {}, [
				h('p', { text: '本应用没有拿到有效的 DSM 登录会话，因此无法读取本机状态。' }),
				h('p', { text: '常见原因有两个：DSM 登录其实没有成功；或者登录 DSM 用的地址与本应用的地址不同——DSM 的会话按地址区分，在别的地址登录不会让本应用生效。' }),
				h('p', { class: 'muted', text: `本应用使用的地址：${window.location.origin}` }),
			]), 'warning'),
			h('div', { class: 'row' }, [
				button('重新登录 DSM', {
					variant: 'primary',
					onclick: () => { window.open('/webman/index.cgi', '_blank'); },
				}),
				button('已确认登录，重试', { onclick: reset }),
			]),
		]);
	}
	return card('暂时无法读取本机状态', [
		banner(`${messageOf(error)}。本机的连接设置没有被修改。`, 'warning'),
		h('div', { class: 'row' }, [
			button('重试', { variant: 'primary', onclick: reset }),
			h('a', { href: '#/logs', text: '查看日志' }),
		]),
	]);
}

/* 2. 首次使用：无 Console 会话且无设备令牌。 */
function renderFirstUse({ reset }) {
	const steps = h('ol', {}, [
		h('li', {}, [ h('strong', { text: '登录' }), h('br'), '使用 EasyTier Console 账号完成设备登录。' ]),
		h('li', {}, [ h('strong', { text: '准备' }), h('br'), '自动下载并安装所需的连接组件。' ]),
		h('li', {}, [ h('strong', { text: '连接' }), h('br'), '选择要让本机加入的私有网络。' ]),
	]);
	const advanced = h('details', {}, [
		h('summary', { text: '高级设置' }),
		h('p', { class: 'muted', text: '已经持有设备注册令牌？可以不登录账号，直接让本机接入网络。' }),
		button('使用设备令牌', { onclick: () => showTokenDialog({ reset }) }),
	]);
	return h('div', {}, [
		card('首次使用', [
			h('p', { text: '把这台 NAS 接入 EasyTier 虚拟网络，只需完成下面三步。' }),
			steps,
			h('div', { class: 'row' }, [
				button('开始使用', { variant: 'primary', onclick: () => startDeviceLogin({ reset }) }),
			]),
		], '本机尚未连接 EasyTier'),
		card('高级方式', [ advanced ], '不使用 Console 账号的连接方式'),
	]);
}

/* 3. 工作空间选择：已登录但尚未完成注册。 */
function renderWorkspaceSetup({ reset, loggedIn }) {
	const children = [
		loggedIn
			? banner('已登录 EasyTier Console。', 'success')
			: banner('本机已保存设备令牌，但还没有完成工作空间设置。', 'info'),
		h('p', { text: '请选择本机所属的 Console 工作空间，并决定使用哪一个注册密钥，其余设置会自动完成。' }),
		h('div', { class: 'row' }, [
			button('继续设置', { variant: 'primary', onclick: () => openWorkspaceDialog({ reset }) }),
			button('退出 Console 登录', { onclick: () => confirmLogout(reset) }),
		]),
	];
	return card('完成本机设置', children, '账号已连接，还差最后一步');
}

/* 账号与本机操作。每个状态都要能进入，否则用户会被卡在当前步骤里：
   例如运行时尚未安装时，除了继续安装没有别的出路，也无法退出登录换账号。 */
function accountCard(status, { reset, loggedIn }, { disconnect = true } = {}) {
	const buttons = [];
	if (status && status.console_url) {
		buttons.push(button('打开 Console', {
			variant: 'primary',
			onclick: () => window.open(consoleWebURL(status.console_url), '_blank', 'noopener'),
		}));
	}
	if (loggedIn) {
		buttons.push(button('退出 Console 登录', { onclick: () => confirmLogout(reset) }));
	}
	if (disconnect) {
		buttons.push(button('断开本机', { variant: 'danger', onclick: () => confirmDisconnect(reset) }));
	}
	const notes = [ '「退出 Console 登录」只清除登录状态，不影响正在运行的连接；「断开本机」会停止服务并删除本机的设备令牌。' ];
	if (status && !status.token_present) {
		notes.push('本机当前还没有设备令牌，断开后可以换一个账号或工作空间重新设置。');
	}
	return card('账号与本机', [
		h('div', { class: 'row' }, buttons),
		...notes.map((text) => h('p', { class: 'muted', text })),
	]);
}

/* 4. 运行时就绪：需要安装 EasyTier 运行时。 */
function renderRuntimeSetup(status, download, { reset, loggedIn }) {
	const running = download.state === 'queued' || download.state === 'running';
	const failed = download.state === 'failed';
	const percent = Math.max(0, Math.min(100, Number(download.percent || 0)));
	const children = [];
	if (running) {
		children.push(
			h('div', { class: 'row row-between' }, [
				h('strong', { text: downloadMessage(download) || '正在准备…' }),
				h('span', { class: 'mono', text: `${percent}%` }),
			]),
			h('div', { class: 'progress' }, [
				h('div', { class: 'progress-fill', style: `width:${percent}%` }),
			]),
			banner('进度会自动更新；安装在本机后台进行，可以放心离开本页面稍后再回来。', 'info'),
		);
	} else {
		if (failed) {
			children.push(banner(
				`${downloadMessage(download) || '安装未完成。'} 账号与本机设置都已保留。`,
				'warning',
			));
		} else {
			children.push(h('p', {
				text: '运行 EasyTier 连接服务前，需要先在本机安装一次运行时。系统会自动下载正确的版本并校验后再安装。',
			}));
		}
		children.push(h('div', { class: 'row' }, [
			button(failed ? '重试安装' : '安装并继续', {
				variant: 'primary',
				onclick: () => startDownload({ reset }),
			}),
		]));
	}
	return h('div', {}, [
		card(
			running ? '正在安装 EasyTier 运行时' : '安装 EasyTier 运行时',
			children,
			'本机准备工作',
		),
		accountCard(status, { reset, loggedIn }),
	]);
}

function startDownload({ reset }) {
	api.downloadStart().then(() => {
		notify('已开始下载并安装运行时。', 'success');
		reset();
	}).catch((error) => {
		if (error && error.code === 'download_busy') {
			reset();
			return;
		}
		notify(messageOf(error), 'error');
	});
}

function pollDownload(host, status, { reset, loggedIn }) {
	const tick = () => {
		api.downloadStatus().then((download) => {
			if (!host.isConnected) {
				return;
			}
			host.replaceChildren(renderRuntimeSetup(status, download, { reset, loggedIn }));
			if (download.state === 'queued' || download.state === 'running') {
				after(1500, tick);
				return;
			}
			if (download.state === 'completed') {
				notify('运行时安装完成。', 'success');
				reset();
			}
		}).catch(() => {
			// 状态查询失败时继续稍后重试，安装本身在后台进行。
			if (host.isConnected) {
				after(3000, tick);
			}
		});
	};
	after(1500, tick);
}

/* 缺少 CAP_NET_ADMIN 时本机只能中继运行：给出管理员一次性授权命令。 */
function tunGrantCommand(status) {
	return `sudo setcap cap_net_admin,cap_net_raw+ep ${runtimeDir(status)}/easytier-core`;
}

/* 缺少 CAP_NET_RAW 时节点能注册但连不上任何 peer：core 默认会把每个出站 socket 绑定到
   承载本地地址的网卡（SO_BINDTODEVICE），Linux 只允许带 CAP_NET_RAW 的进程这么做。
   这是比「没有虚拟网卡」更严重的问题，因此单独提示。 */
function bindNotice(status) {
	if (!status.core_installed || status.bind_capable) {
		return null;
	}
	const command = `sudo setcap cap_net_raw+ep ${runtimeDir(status)}/easytier-core`;
	return banner(h('div', {}, [
		h('p', { text: '本机缺少 CAP_NET_RAW，节点虽然能注册并显示在线，但无法连接任何 peer（节点列表为空）。' }),
		h('p', { class: 'muted', text: '这是 EasyTier 默认把出站 socket 绑定到网卡所需的权限。请以管理员身份执行下面这条命令，然后重新启动套件：' }),
		h('div', { class: 'row' }, [
			h('code', { class: 'mono', text: command }),
			button('复制命令', {
				onclick: () => {
					navigator.clipboard.writeText(command)
						.then(() => notify('命令已复制。', 'success'))
						.catch(() => notify('复制失败，请手动选择命令文本。', 'error'));
				},
			}),
		]),
	]), 'error');
}

function runtimeDir(status) {
	return status.install_dir || '/volume1/@appdata/easytier-pro/runtime';
}

function tunNotice(status) {
	if (!status.core_installed || status.tun_capable) {
		return null;
	}
	const command = tunGrantCommand(status);
	return banner(h('div', {}, [
		h('p', { text: '本机当前以「无 TUN 模式」运行：DSM 不允许套件以 root 运行，套件因此拿不到创建虚拟网卡的权限。' }),
		h('p', {}, [
			'这不会让本机退出网络：本机仍会获得虚拟 IP，其它节点可以主动访问本机（也能经本机虚拟 IP 访问本机上的服务），',
			'并且照常可以做子网路由和中继。',
			h('br'),
			'唯一的区别是本机自身没有虚拟网卡，因此 NAS 上的程序无法主动访问网络里的其它节点。',
		]),
		h('p', {}, [
			'已自动在 EasyTier Console 上把本机节点设为「无 TUN 模式」，这样下发的配置才与本机权限一致。',
			h('br'),
			'授予权限后本机会自动取消该设置，恢复完整模式。',
		]),
		h('p', { class: 'muted', text: '需要虚拟 IP 时，请以管理员身份执行下面这条命令（SSH，或用「控制面板 → 任务计划」新建一个以 root 运行的脚本任务），然后重新启动套件：' }),
		h('div', { class: 'row' }, [
			h('code', { class: 'mono', text: command }),
			button('复制命令', {
				onclick: () => {
					navigator.clipboard.writeText(command)
						.then(() => notify('命令已复制。', 'success'))
						.catch(() => notify('复制失败，请手动选择命令文本。', 'error'));
				},
			}),
		]),
		h('p', { class: 'muted', text: '提示：每次重新下载运行时后，新文件都会丢失该权限，本页会再次显示这条提示。' }),
	]), 'warning');
}

/* 5. 服务停止：运行时已安装且已配置，但服务未运行。 */
function renderServiceStopped(status, { reset, loggedIn }) {
	const startButton = button('启动连接', {
		variant: 'primary',
		onclick: () => {
			startButton.disabled = true;
			api.serviceAction('start').then(() => {
				notify('连接已启动。', 'success');
				reset();
			}).catch((error) => {
				startButton.disabled = false;
				notify(messageOf(error), 'error');
			});
		},
	});
	return h('div', {}, [
		card('启动连接', [
			bindNotice(status),
			tunNotice(status),
			h('p', { text: 'EasyTier 已安装，本机也已与账号关联。启动连接后，本机就会加入所选的私有网络。' }),
			h('div', { class: 'row' }, [ startButton ]),
		].filter(Boolean), '本机已准备就绪'),
		accountCard(status, { reset, loggedIn }),
	]);
}

/* 6. 运行中：状态摘要 + 网络列表 + 账号操作。 */
function renderRunning(status, auth, summary, networks, { reset, loggedIn }) {
	if (!summary) {
		return card('连接状态', [
			banner('无法读取本机连接详情，连接服务可能刚启动或已停止。可以稍后刷新，或先重启连接。', 'warning'),
			h('div', { class: 'row' }, [
				button('刷新', { onclick: reset }),
				button('重启连接', { onclick: () => serviceAction('restart', reset) }),
			]),
		]);
	}

	const sections = [];
	const ipv4 = localIPv4(summary);
	const peers = peerCount(summary.peers);
	const interfaces = Array.isArray(summary.interfaces) ? summary.interfaces.filter(Boolean) : [];
	const joined = networks && networks.machine && Array.isArray(networks.machine.networks)
		? networks.machine.networks
		: [];
	const summaryCard = card('连接状态', [
		bindNotice(status),
		tunNotice(status),
		detailList([
			[ '运行模式', status.tun_capable ? '完整模式（本机有虚拟网卡）' : '无 TUN 模式（用户态转发）' ],
			[ '本机虚拟 IP', ipv4 ? h('span', { class: 'mono', text: ipv4 }) : '' ],
			[ 'Peer 数', String(peers) ],
			[ '虚拟网卡', interfaces.length ? interfaces.join(', ') : (status.tun_capable ? '' : '无（无 TUN 模式）') ],
			[ 'Core 版本', valueOrDash(status.core_version) ],
			[ 'CLI 版本', valueOrDash(status.cli_version) ],
		]),
	].filter(Boolean));
	sections.push(summaryCard);

	if (!networks) {
		sections.push(card('网络', [
			banner('暂时无法从 Console 读取网络列表；本机已建立的隧道不受影响。', 'warning'),
			h('div', { class: 'row' }, [ button('重试', { onclick: reset }) ]),
		]));
	} else {
		sections.push(renderNetworkCard(networks, joined, reset));
	}

	sections.push(accountCard(status, { reset, loggedIn }));
	return h('div', {}, sections);
}

function renderNetworkCard(networks, joined, reset) {
	const memberships = new Map();
	for (const item of joined) {
		if (item && item.id) {
			memberships.set(item.id, item);
		}
	}
	const catalog = Array.isArray(networks.networks) ? networks.networks.slice() : [];
	for (const item of joined) {
		if (item && item.id && !catalog.some((candidate) => candidate && candidate.id === item.id)) {
			catalog.push(item);
		}
	}
	if (!networks.enrolled && catalog.length === 0) {
		return card('网络', [
			banner('本机正在向 Console 注册，网络列表稍后才会出现，请稍候刷新。', 'info'),
			h('div', { class: 'row' }, [ button('刷新', { onclick: reset }) ]),
		]);
	}
	const rows = catalog.map((network) => {
		const membership = memberships.get(network.id);
		const action = membership
			? button(`退出 ${networkName(network)}`, {
				variant: 'danger',
				onclick: () => confirmLeaveNetwork(network, reset),
			})
			: button(`加入 ${networkName(network)}`, {
				variant: 'primary',
				onclick: () => joinNetwork(network, reset),
			});
		return [
			h('strong', { text: networkName(network) }),
			network.ipv4_cidr ? h('span', { class: 'mono', text: network.ipv4_cidr }) : '—',
			membership
				? h('span', { class: 'mono', text: membership.node_ipv4 || membership.ipv4_addr || membership.virtual_ip || '已加入' })
				: h('span', { class: 'muted', text: '未加入' }),
			action,
		];
	});
	return card('网络', [
		h('p', { class: 'muted', text: '可以分别加入或退出每个网络，互不影响。' }),
		table([ '名称', 'IPv4 段', '本机状态', '操作' ], rows, { empty: '该工作空间还没有网络，可先在 Console 网页中创建。' }),
	]);
}

function joinNetwork(network, reset) {
	api.networkJoin(network.id).then((result) => {
		showOperationProgress(`正在加入「${networkName(network)}」`, result.operation_id, {
			completed: () => {
				notify(`本机已加入「${networkName(network)}」。`, 'success');
				reset();
			},
			failed: reset,
		});
	}).catch((error) => notify(messageOf(error), 'error'));
}

function confirmLeaveNetwork(network, reset) {
	const view = modalDialog(`退出「${networkName(network)}」？`, h('div', {}, [
		h('p', { text: '本机将退出该网络，其它已加入的网络不受影响。' }),
	]), [
		button('取消', { onclick: () => view.close() }),
		button('退出网络', {
			variant: 'danger',
			onclick: () => {
				view.close();
				api.networkLeave(network.id).then((result) => {
					showOperationProgress(`正在退出「${networkName(network)}」`, result.operation_id, {
						completed: () => {
							notify(`本机已退出「${networkName(network)}」。`, 'success');
							reset();
						},
						failed: reset,
					});
				}).catch((error) => notify(messageOf(error), 'error'));
			},
		}),
	]);
}

function serviceAction(action, reset) {
	api.serviceAction(action).then(() => {
		notify('操作已完成。', 'success');
		reset();
	}).catch((error) => notify(messageOf(error), 'error'));
}

function confirmLogout(reset) {
	const view = modalDialog('退出 Console 登录？', h('div', {}, [
		h('p', { text: '只会清除本机保存的 Console 登录状态，已配置的连接和正在运行的隧道不受影响。' }),
	]), [
		button('取消', { onclick: () => view.close() }),
		button('退出登录', {
			onclick: () => {
				view.close();
				api.authLogout().then(() => {
					notify('已退出 Console 登录。', 'success');
					reset();
				}).catch((error) => notify(messageOf(error), 'error'));
			},
		}),
	]);
}

function confirmDisconnect(reset) {
	const view = modalDialog('断开本机？', h('div', {}, [
		h('p', { text: '将停止本机的 EasyTier 服务并删除保存的设备令牌；Console 登录状态会保留。之后需要重新连接才能再次加入网络。' }),
	]), [
		button('取消', { onclick: () => view.close() }),
		button('断开本机', {
			variant: 'danger',
			onclick: () => {
				view.close();
				api.disconnect().then((result) => {
					showOperationProgress('正在断开本机', result.operation_id, {
						completed: () => {
							notify('本机已断开连接。', 'success');
							reset();
						},
						failed: reset,
					});
				}).catch((error) => notify(messageOf(error), 'error'));
			},
		}),
	]);
}

/* 设备码登录流程。 */
function startDeviceLogin({ reset }) {
	const pendingView = modalDialog('登录 EasyTier Console', h('p', { text: '正在向 Console 申请设备码…' }));
	api.authStart().then((result) => {
		pendingView.close();
		const target = result.verification_uri_complete || result.verification_uri;
		let stopped = false;
		const view = modalDialog('登录 EasyTier Console', h('div', {}, [
			h('p', { text: '请在浏览器中打开下面的授权页面，并输入验证码完成登录。' }),
			h('p', { class: 'row' }, [
				h('strong', { text: '验证码：' }),
				h('span', { class: 'mono', text: result.user_code || '' }),
			]),
			h('p', {}, [
				h('a', {
					href: target,
					target: '_blank',
					rel: 'noreferrer noopener',
					text: result.verification_uri || target,
				}),
			]),
			h('p', { class: 'muted', text: '页面会自动检测登录结果，完成后此窗口将自动关闭。' }),
		]), [
			button('取消', {
				onclick: () => {
					stopped = true;
					view.close();
				},
			}),
			button('打开授权页面', {
				variant: 'primary',
				onclick: () => window.open(target, '_blank', 'noopener'),
			}),
		]);
		const poll = (delay) => {
			after(Math.max(1, delay) * 1000, () => {
				if (stopped) {
					return;
				}
				api.authPoll().then((pollResult) => {
					if (pollResult.authenticated) {
						stopped = true;
						view.close();
						notify('已成功登录 EasyTier Console。', 'success');
						reset();
						return;
					}
					poll(Number(pollResult.retry_after) || 5);
				}).catch((error) => {
					stopped = true;
					view.close();
					if (needsRelogin(error)) {
						notify('请重新登录 DSM 后再试。', 'error');
						reset();
						return;
					}
					showLoginError(messageOf(error), { reset });
				});
			});
		};
		poll(Number(result.interval) || 5);
	}).catch((error) => {
		pendingView.close();
		notify(messageOf(error), 'error');
	});
}

function showLoginError(message, { reset }) {
	const view = modalDialog('登录未完成', h('div', {}, [
		banner(message, 'warning'),
		h('p', { text: '可以重新开始设备登录，或稍后重试。' }),
	]), [
		button('关闭', { onclick: () => view.close() }),
		button('重新开始', {
			variant: 'primary',
			onclick: () => {
				view.close();
				startDeviceLogin({ reset });
			},
		}),
	]);
}

/* 直接粘贴设备令牌连接。 */
function showTokenDialog({ reset }) {
	const tokenInput = h('input', { type: 'password', autocomplete: 'off' });
	const serverInput = h('input', {
		type: 'text',
		placeholder: '可留空，将自动从 Console 读取',
	});
	const view = modalDialog('使用设备令牌', h('div', {}, [
		h('p', { text: '粘贴从 EasyTier Console 获取的设备注册令牌。令牌只保存在本机，仅 root 可读。' }),
		h('div', { class: 'field' }, [
			h('label', { text: '设备令牌' }),
			tokenInput,
		]),
		h('div', { class: 'field' }, [
			h('label', { text: '配置服务器（可选）' }),
			serverInput,
			h('small', { text: '例如 tcp://et-web.console.easytier.net:22020；留空时自动获取。' }),
		]),
	]), [
		button('取消', { onclick: () => view.close() }),
		button('连接', {
			variant: 'primary',
			onclick: () => {
				const token = tokenInput.value.trim();
				if (!token) {
					notify('请输入设备令牌。', 'error');
					return;
				}
				view.close();
				api.connectToken(token, serverInput.value.trim()).then((result) => {
					showOperationProgress('正在连接本机', result.operation_id, {
						completed: (operation) => {
							if (operation.needs_download) {
								notify('令牌已保存，接下来需要安装运行时。', 'success');
							} else {
								notify('本机已开始连接。', 'success');
							}
							reset();
						},
						failed: reset,
					});
				}).catch((error) => notify(messageOf(error), 'error'));
			},
		}),
	]);
}

/* 工作空间 + 注册密钥选择。 */
function openWorkspaceDialog({ reset }) {
	api.authMe().then((result) => {
		const account = result && result.account;
		const tenants = account && Array.isArray(account.tenants) ? account.tenants : [];
		if (tenants.length === 0) {
			notify('当前账号没有可用的工作空间。', 'error');
			return;
		}
		if (tenants.length === 1) {
			loadEnrollmentOptions(tenants[0], { reset });
			return;
		}
		const list = h('div', {}, tenants.map((tenant) => h('p', { class: 'row' }, [
			button(tenant.name || tenant.slug || tenant.id, {
				onclick: () => {
					view.close();
					loadEnrollmentOptions(tenant, { reset });
				},
			}),
			tenant.slug && tenant.name ? h('small', { class: 'muted', text: tenant.slug }) : null,
			tenant.role ? h('small', { class: 'muted', text: tenant.role }) : null,
		])));
		const view = modalDialog('选择工作空间', h('div', {}, [
			h('p', { text: '请选择本机要加入的 Console 工作空间。' }),
			list,
		]), [
			button('取消', { onclick: () => view.close() }),
		]);
	}).catch((error) => notify(messageOf(error), 'error'));
}

function loadEnrollmentOptions(workspace, { reset }) {
	const workspaceID = workspace.id || workspace.slug;
	const pendingView = modalDialog('读取注册密钥', h('p', { text: '正在检查本机与该工作空间的注册密钥…' }));
	api.enrollmentOptions(workspaceID).then((options) => {
		pendingView.close();
		showEnrollmentDialog(workspace, options, { reset });
	}).catch((error) => {
		pendingView.close();
		notify(messageOf(error), 'error');
	});
}

function showEnrollmentDialog(workspace, options, { reset }) {
	const workspaceID = workspace.id || workspace.slug;
	const workspaceName = workspace.name || workspace.slug || workspaceID;
	const choices = [];

	if (options.local_connection) {
		choices.push({
			mode: 'local',
			keyID: '',
			label: '使用本机已保存的密钥',
			hint: '沿用本机之前保存的注册密钥完成设置。',
		});
	}
	if (options.existing_device && options.current_key) {
		choices.push({
			mode: 'recover',
			keyID: options.current_key.id,
			label: `恢复当前设备密钥：${enrollmentKeyName(options.current_key)}`,
			hint: '找回这台设备之前在 Console 上使用的密钥。',
		});
	}
	const reusableKeys = Array.isArray(options.reusable_keys) ? options.reusable_keys.slice() : [];
	reusableKeys.sort((left, right) => Number(Boolean(right.pre_approved)) - Number(Boolean(left.pre_approved)));
	for (const key of reusableKeys) {
		const used = Math.max(0, Number(key.used_count || 0));
		const approval = key.pre_approved ? '' : '，需要管理员审批';
		choices.push({
			mode: 'existing',
			keyID: key.id,
			label: `使用已有共享密钥：${enrollmentKeyName(key)}（已使用 ${used} 次${approval}）`,
			hint: '多台设备可以共用同一个密钥。',
		});
	}
	choices.push({
		mode: 'shared',
		keyID: '',
		label: '新建共享密钥',
		hint: '创建一个可重复使用的密钥，之后其它设备也可以用它加入。',
	});
	choices.push({
		mode: 'dedicated',
		keyID: '',
		label: '新建专用密钥',
		hint: '创建一个只属于这台设备的密钥，撤销时只影响本机。',
	});

	const radios = choices.map((choice, index) => h('label', { class: 'checkbox' }, [
		h('input', {
			type: 'radio',
			name: 'enrollment-choice',
			value: String(index),
			checked: index === 0 ? true : null,
		}),
		h('span', {}, [
			choice.label,
			h('br'),
			h('small', { class: 'muted', text: choice.hint }),
		]),
	]));

	const body = h('div', {}, [
		h('p', { text: `请为本机选择在工作空间「${workspaceName}」中使用的注册密钥。` }),
		options.current_key_unavailable
			? banner('当前设备密钥已不可用，请在下面选择其它密钥。', 'warning')
			: null,
		h('div', { class: 'field' }, radios),
		h('p', { class: 'muted', text: '注意：撤销共享密钥会让所有使用该密钥的设备断开连接。' }),
	]);

	const view = modalDialog('选择注册密钥', body, [
		button('取消', { onclick: () => view.close() }),
		button('继续', {
			variant: 'primary',
			onclick: () => {
				const selected = body.querySelector('input[name="enrollment-choice"]:checked');
				const choice = choices[selected ? Number(selected.value) : 0] || choices[0];
				view.close();
				api.activate(workspaceID, choice.mode, choice.keyID).then((result) => {
					showOperationProgress('正在完成本机设置', result.operation_id, {
						completed: (operation) => {
							if (operation.needs_download) {
								notify('注册完成，接下来需要安装运行时。', 'success');
							} else {
								notify('本机设置已完成。', 'success');
							}
							reset();
						},
						failed: reset,
					});
				}).catch((error) => notify(messageOf(error), 'error'));
			},
		}),
	]);
}

