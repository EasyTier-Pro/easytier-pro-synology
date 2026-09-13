// 网络页面：选择一个 Console 网络并查看其中的节点（只读）。
import { h, mount, card, table, detailList, button, banner, loading, errorBox, notify, valueOrDash, formatRemoteTime } from '../lib/dom.js';
import { api, needsRelogin } from '../lib/api.js';

function networkName(network) {
	return network.name || network.network_name || network.id;
}

function nodeName(node) {
	return node.display_name || node.hostname || node.machine_id || node.id;
}

/** nodeStatus maps Console state fields to one short Chinese label. */
function nodeStatus(node) {
	if (node.status === 'error') {
		return '错误';
	}
	if (node.lifecycle_state === 'delete_pending') {
		return '移出中';
	}
	if (node.lifecycle_state === 'attach_pending') {
		return '加入中';
	}
	if (node.status === 'provisioning') {
		return '配置中';
	}
	if (node.status === 'removed' || node.lifecycle_state === 'deleted') {
		return '已移除';
	}
	const online = node.status || node.connectivity_state || node.runtime_health_status || '';
	if (online === 'online' || online === 'healthy') {
		return '在线';
	}
	if (online === 'offline' || online === 'device_offline') {
		return '离线';
	}
	return online || '—';
}

function nodeRole(node) {
	if (node.is_exit_node) {
		return '出口节点';
	}
	if (node.is_subnet_router) {
		return '子网路由';
	}
	return '普通节点';
}

function messageOf(error) {
	return error && error.message ? error.message : String(error);
}

/** reloginHint renders the DSM session notice when the API demands re-login. */
function reloginHint() {
	return banner('请重新登录 DSM 后再使用本页面。', 'warning');
}

export async function render() {
	const root = h('div', { class: 'page' });

	let data;
	try {
		data = await api.networks();
	} catch (error) {
		if (error && error.code === 'no_workspace') {
			root.appendChild(card('网络', [
				banner('尚未选择 Console 工作空间，请先到「设置」页面完成工作空间配置。', 'warning'),
				h('div', { class: 'row' }, [
					button('前往设置', {
						variant: 'primary',
						onclick: () => {
							window.location.hash = '#/settings';
						},
					}),
				]),
			], '选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。'));
			return root;
		}
		if (error && error.code === 'not_authenticated') {
			root.appendChild(card('网络', [
				banner('请先登录 EasyTier Console，登录入口在「概览」页面。', 'warning'),
				h('div', { class: 'row' }, [
					button('前往概览', {
						variant: 'primary',
						onclick: () => {
							window.location.hash = '#/overview';
						},
					}),
				]),
			], '选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。'));
			return root;
		}
		if (needsRelogin(error)) {
			root.appendChild(card('网络', [ reloginHint() ]));
			return root;
		}
		notify(messageOf(error), 'error');
		root.appendChild(card('网络', [
			errorBox(`无法读取工作空间网络：${messageOf(error)}`),
		], '选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。'));
		return root;
	}

	const machineID = data.machine_id || '';
	const networks = Array.isArray(data.networks) ? data.networks : [];

	if (networks.length === 0) {
		root.appendChild(card('网络', [
			banner('当前工作空间还没有任何网络，请先在 EasyTier Console 网页控制台中创建网络。', 'info'),
		], '选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。'));
		return root;
	}

	const state = {
		networks,
		selectedID: networks[0].id,
		selectedNodeID: null,
		nodes: [],
		generation: 0,
	};

	const selector = h('select', {
		onchange: (event) => {
			state.selectedID = event.target.value;
			state.selectedNodeID = null;
			loadNodes();
		},
	}, networks.map((network) => h('option', { value: network.id, text: networkName(network) })));
	selector.value = state.selectedID;

	const refreshButton = button('刷新', { onclick: () => loadNodes() });

	const errorHost = h('div');
	const tableHost = h('div');
	const detailHost = h('div');

	function renderDetail() {
		const node = state.nodes.find((entry) => entry.id === state.selectedNodeID);
		if (!node) {
			mount(detailHost, null);
			return;
		}
		const isLocal = Boolean(machineID) && node.machine_id === machineID;
		const system = [ node.os, node.os_version ].filter(Boolean).join(' ');
		mount(detailHost, card(`节点详情：${nodeName(node)}`, [
			detailList([
				[ '主机名', node.hostname ],
				[ 'DNS 名称', node.dns_name ],
				[ '状态', nodeStatus(node) ],
				[ '系统', system ],
				[ 'EasyTier 版本', node.version ],
				[ '最后在线时间', formatRemoteTime(node.last_seen) ],
				[ '所属', isLocal ? '本机（这台 NAS）' : '其他设备' ],
			]),
		]));
	}

	function renderTable() {
		const rows = state.nodes.map((node) => {
			const isLocal = Boolean(machineID) && node.machine_id === machineID;
			const isSelected = node.id === state.selectedNodeID;
			return [
				h('a', {
					href: '#',
					class: isSelected ? '' : undefined,
					onclick: (event) => {
						event.preventDefault();
						state.selectedNodeID = node.id;
						renderTable();
						renderDetail();
					},
				}, [ h('strong', { text: nodeName(node) }), isLocal ? '（本机）' : '' ]),
				valueOrDash(node.ipv4_addr),
				nodeStatus(node),
				nodeRole(node),
			];
		});
		mount(tableHost, table([ '名称', '虚拟 IP', '状态', '角色' ], rows, { empty: '该网络下还没有节点。' }));
	}

	async function loadNodes() {
		const generation = ++state.generation;
		const networkID = state.selectedID;
		const network = state.networks.find((entry) => entry.id === networkID);
		mount(errorHost, null);
		mount(detailHost, null);
		mount(tableHost, loading('正在加载节点…'));
		try {
			const result = await api.networkNodes(networkID);
			if (generation !== state.generation) {
				return;
			}
			state.nodes = Array.isArray(result.nodes) ? result.nodes : [];
			renderTable();
		} catch (error) {
			if (generation !== state.generation) {
				return;
			}
			state.nodes = [];
			mount(tableHost, null);
			if (needsRelogin(error)) {
				mount(errorHost, reloginHint());
				return;
			}
			notify(messageOf(error), 'error');
			mount(errorHost, banner(`无法读取「${network ? networkName(network) : '所选网络'}」的节点：${messageOf(error)}`, 'error'));
		}
	}

	root.appendChild(card('网络', [
		h('div', { class: 'row' }, [
			h('div', { class: 'field' }, [
				h('label', { for: 'networks-page-select', text: '网络' }),
				selector,
			]),
			refreshButton,
		]),
	], '选择一个网络以查看其中的节点。本页面为只读，加入或退出网络请在「概览」页面操作。'));

	selector.id = 'networks-page-select';

	const nodesCard = card('节点', [ errorHost, tableHost ]);
	const detailCardHost = h('div', {}, [ detailHost ]);
	root.appendChild(nodesCard);
	root.appendChild(detailCardHost);

	loadNodes();
	return root;
}

export function unmount() {
	// 本页面没有定时器或轮询需要清理。
}
