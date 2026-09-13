/** valueOrDash renders empty values as a dash. */
export function valueOrDash(value: unknown): string {
	if (value === null || value === undefined || value === '') {
		return '—'
	}
	return String(value)
}

/** formatRemoteTime renders an RFC3339 string. */
export function formatRemoteTime(value?: string | null): string {
	if (!value) {
		return '—'
	}
	const parsed = new Date(value)
	if (Number.isNaN(parsed.getTime())) {
		return String(value)
	}
	return parsed.toLocaleString('zh-CN', { hour12: false })
}

/** networkName falls back through the names a Console payload may carry. */
export function networkName(network?: { name?: string; network_name?: string; id?: string } | null): string {
	return (network && (network.name || network.network_name || network.id)) || '未命名网络'
}

/** nodeName prefers the operator-set display name. */
export function nodeName(node?: { hostname?: string; display_name?: string; id?: string; machine_id?: string } | null): string {
	if (!node) {
		return '未命名节点'
	}
	return node.hostname || node.display_name || node.id || node.machine_id || '未命名节点'
}

/** enrollmentKeyName renders one enrollment key for a choice list. */
export function enrollmentKeyName(key?: { display_name?: string; key_code?: string; id?: string } | null): string {
	const name = key && (key.display_name || key.key_code || key.id)
	if (key && key.key_code && key.key_code !== name) {
		return `${name} · ${key.key_code}`
	}
	return name || '未命名密钥'
}

/** nodeStatus maps the daemon's node state onto operator-facing text. */
export function nodeStatus(node?: {
	status?: string
	lifecycle_state?: string
	connectivity_state?: string
	runtime_health_status?: string
} | null): string {
	if (!node) {
		return '—'
	}
	if (node.status === 'error') {
		return '错误'
	}
	if (node.lifecycle_state === 'delete_pending') {
		return '移出中'
	}
	if (node.lifecycle_state === 'attach_pending') {
		return '加入中'
	}
	if (node.status === 'provisioning') {
		return '配置中'
	}
	if (node.status === 'removed' || node.lifecycle_state === 'deleted') {
		return '已移除'
	}
	const online = node.status || node.connectivity_state || node.runtime_health_status || ''
	if (online === 'online' || online === 'healthy') {
		return '在线'
	}
	if (online === 'offline' || online === 'device_offline') {
		return '离线'
	}
	return online || '—'
}

/** nodeRole describes what the node does in its network. */
export function nodeRole(node?: { is_exit_node?: boolean; is_subnet_router?: boolean } | null): string {
	if (node && node.is_exit_node) {
		return '出口节点'
	}
	if (node && node.is_subnet_router) {
		return '子网路由'
	}
	return '普通节点'
}
