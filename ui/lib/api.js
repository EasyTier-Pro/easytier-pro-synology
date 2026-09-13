// Local API client. Every request goes to the same origin as this page, so the
// browser attaches the DSM session cookie automatically.

const errorMessages = {
	access_denied: 'Console 拒绝了本次操作。',
	account_lookup_failed: '已登录，但无法读取账号信息。',
	connection_change_busy: '已有本机设置变更在进行中。',
	connection_change_interrupted: '上次设置变更被中断，请重试。',
	connection_change_not_found: '该操作记录已不存在。',
	connection_change_start_failed: '无法在后台启动本机设置变更。',
	console_unreachable: '无法连接 EasyTier Console。',
	core_not_installed: '请先安装 EasyTier 运行时。',
	device_auth_failed: '设备授权失败。',
	download_busy: '已有更新在进行中。',
	dsm_auth_forbidden: '只有 DSM 管理员可以使用该功能。',
	dsm_auth_required: '请先登录 DSM。',
	enrollment_choice_required: '请选择注册密钥。',
	enrollment_failed: '无法创建设备注册密钥。',
	enrollment_key_unavailable: '所选注册密钥已不可用。',
	enrollment_lookup_failed: '无法从 EasyTier Console 读取注册密钥。',
	expired_token: '设备码已过期。',
	invalid_action: '服务操作无效。',
	invalid_auth_state: '登录状态无效，请重新登录。',
	invalid_bootstrap_token: '请输入有效的设备注册令牌。',
	invalid_config_server: '配置服务器地址无效。',
	invalid_console_response: 'EasyTier Console 返回了无效响应。',
	invalid_console_url: 'Console 地址无效。',
	invalid_connection_operation: '该操作编号无效。',
	invalid_enrollment_mode: '请选择注册密钥的使用方式。',
	invalid_install_dir: '运行目录无效。',
	invalid_network: '请选择有效的网络。',
	invalid_request: '请求无效。',
	invalid_workspace: '请选择有效的 Console 工作空间。',
	method_not_found: '不支持的操作。',
	network_join_failed: 'Console 无法把本机加入该网络。',
	network_leave_failed: 'Console 无法把本机移出该网络。',
	network_lookup_failed: '无法读取工作空间网络。',
	no_bootstrap_token: '请先连接本机。',
	no_device_auth: '请先开始 Console 登录。',
	no_workspace: '请先选择 Console 工作空间。',
	node_lookup_failed: '无法读取网络节点。',
	not_authenticated: '请先登录 EasyTier Console。',
	release_lookup_failed: '无法读取 EasyTier 稳定版本。',
	router_not_enrolled: '请先启动 EasyTier Pro 并等待本机注册。',
	service_not_running: 'EasyTier Pro 未运行。',
	service_restart_failed: 'EasyTier Pro 重启失败。',
	service_start_failed: 'EasyTier Pro 启动失败。',
	state_unavailable: '本机状态存储不可用。',
	unknown_error: '操作失败。',
	workspace_access_denied: '当前账号无法使用所选工作空间。',
};

/** ApiError carries the stable error code from the backend. */
export class ApiError extends Error {
	constructor(code, message) {
		super(message || errorMessages[code] || `操作失败：${code}`);
		this.code = code;
	}
}

/** needsRelogin reports whether the DSM session has to be renewed. */
export function needsRelogin(error) {
	return error instanceof ApiError && (error.code === 'dsm_auth_required' || error.code === 'dsm_auth_forbidden');
}

async function request(path, options = {}) {
	const init = {
		method: options.method || 'GET',
		headers: { 'X-Easytier-Request': '1' },
		credentials: 'same-origin',
	};
	if (options.body !== undefined) {
		init.headers['Content-Type'] = 'application/json';
		init.body = JSON.stringify(options.body);
	}
	let response;
	try {
		response = await fetch(path, init);
	} catch (error) {
		throw new ApiError('console_unreachable', '无法连接本机 EasyTier Pro 服务。');
	}
	if (response.status === 401 || response.status === 403) {
		throw new ApiError(response.status === 403 ? 'dsm_auth_forbidden' : 'dsm_auth_required');
	}
	let payload;
	try {
		payload = await response.json();
	} catch (error) {
		throw new ApiError('invalid_console_response', '本机服务返回了无效响应。');
	}
	if (payload.ok === false) {
		throw new ApiError(payload.error || 'unknown_error', payload.message);
	}
	return payload;
}

function query(params) {
	const search = new URLSearchParams();
	for (const [ key, value ] of Object.entries(params)) {
		if (value !== undefined && value !== null && value !== '') {
			search.set(key, value);
		}
	}
	const text = search.toString();
	return text ? `?${text}` : '';
}

export const api = {
	status: () => request('api/status'),
	localSummary: () => request('api/local-summary'),
	serviceAction: (action) => request('api/service/action', { method: 'POST', body: { action } }),
	applySettings: (settings) => request('api/settings', { method: 'POST', body: settings }),
	authStatus: () => request('api/auth/status'),
	authStart: () => request('api/auth/device/start', { method: 'POST' }),
	authPoll: () => request('api/auth/device/poll', { method: 'POST' }),
	authMe: () => request('api/auth/me'),
	authLogout: () => request('api/auth/logout', { method: 'POST' }),
	enrollmentOptions: (workspace) => request(`api/workspaces/${encodeURIComponent(workspace)}/enrollment-options`),
	activate: (workspace, enrollmentMode, enrollmentKeyID) => request(
		`api/workspaces/${encodeURIComponent(workspace)}/activate`,
		{ method: 'POST', body: { enrollment_mode: enrollmentMode, enrollment_key_id: enrollmentKeyID || '' } },
	),
	connectToken: (bootstrapToken, configServer) => request('api/connect-token', {
		method: 'POST',
		body: { bootstrap_token: bootstrapToken, config_server: configServer || '' },
	}),
	disconnect: () => request('api/disconnect', { method: 'POST' }),
	operationStatus: (operationID) => request(`api/connection-change/status${query({ operation_id: operationID })}`),
	networks: () => request('api/networks'),
	networkNodes: (networkID) => request(`api/networks/${encodeURIComponent(networkID)}/nodes`),
	networkJoin: (networkID) => request(`api/networks/${encodeURIComponent(networkID)}/join`, { method: 'POST' }),
	networkLeave: (networkID) => request(`api/networks/${encodeURIComponent(networkID)}/leave`, { method: 'POST' }),
	downloadStart: (version) => request('api/runtime/download', { method: 'POST', body: { version: version || '' } }),
	downloadStatus: () => request('api/runtime/download/status'),
	logs: (lines) => request(`api/logs${query({ lines })}`),
};

/**
 * consoleWebURL derives the Console web console address from the configured
 * API address, matching the desktop client.
 */
export function consoleWebURL(consoleURL) {
	let base;
	try {
		base = new URL(consoleURL || 'https://api.console.easytier.net');
	} catch (error) {
		return 'https://console.easytier.net/';
	}
	const host = base.hostname === 'api.console.easytier.net' ? 'console.easytier.net' : base.hostname;
	return `${base.protocol}//${host}${base.port ? `:${base.port}` : ''}/`;
}
