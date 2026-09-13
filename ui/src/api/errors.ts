// Stable error codes the daemon returns, mapped to Chinese text for the cases
// where it does not send a message of its own. The codes are part of the daemon
// contract, so this table is the interface's own copy of it.
const errorMessages: Record<string, string> = {
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
	relay_mode_failed: '无法在 EasyTier Console 上切换本机的运行模式。',
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
}

/** ApiError carries the stable error code from the backend. */
export class ApiError extends Error {
	readonly code: string

	constructor(code: string, message?: string) {
		super(message || errorMessages[code] || `操作失败：${code}`)
		this.name = 'ApiError'
		this.code = code
	}
}

/** needsRelogin reports whether the DSM session has to be renewed. */
export function needsRelogin(error: unknown): boolean {
	return error instanceof ApiError
		&& (error.code === 'dsm_auth_required' || error.code === 'dsm_auth_forbidden')
}

/** messageOf renders any thrown value as text for the interface. */
export function messageOf(error: unknown): string {
	if (error && typeof error === 'object' && 'message' in error) {
		return String((error as { message: unknown }).message)
	}
	return String(error)
}
