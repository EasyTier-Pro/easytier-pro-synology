import { describe, expect, it } from 'vitest'
import { enrollmentKeyName, networkName, nodeName, nodeRole, nodeStatus, valueOrDash } from './format'

describe('node names', () => {
	// A node renamed in the Console must be listed under that name: the display
	// name is what the operator set, the hostname is what the machine reports.
	it('prefers the display name over the hostname', () => {
		expect(nodeName({ display_name: '办公 NAS', hostname: 'synology-nas' })).toBe('办公 NAS')
		expect(nodeName({ hostname: 'synology-nas' })).toBe('synology-nas')
		expect(nodeName({ machine_id: 'm1' })).toBe('m1')
		expect(nodeName(null)).toBe('未命名节点')
	})
})

describe('node state', () => {
	it('reports a lifecycle in progress before the connection state', () => {
		expect(nodeStatus({ status: 'online', lifecycle_state: 'delete_pending' })).toBe('移出中')
		expect(nodeStatus({ status: 'online', lifecycle_state: 'attach_pending' })).toBe('加入中')
	})
	it('translates the connection states', () => {
		expect(nodeStatus({ status: 'online' })).toBe('在线')
		expect(nodeStatus({ connectivity_state: 'healthy' })).toBe('在线')
		expect(nodeStatus({ status: 'offline' })).toBe('离线')
	})
	it('shows a dash for a node with no state', () => {
		expect(nodeStatus({})).toBe('—')
	})
})

describe('node roles', () => {
	it('prefers the exit node', () => {
		expect(nodeRole({ is_exit_node: true, is_subnet_router: true })).toBe('出口节点')
		expect(nodeRole({ is_subnet_router: true })).toBe('子网路由')
		expect(nodeRole({})).toBe('普通节点')
	})
})

describe('names and values', () => {
	it('falls back through the names a network may carry', () => {
		expect(networkName({ name: 'A' })).toBe('A')
		expect(networkName({ network_name: 'B' })).toBe('B')
		expect(networkName({ id: 'c' })).toBe('c')
		expect(networkName(null)).toBe('未命名网络')
	})
	it('renders the key code next to a key name only when it adds something', () => {
		expect(enrollmentKeyName({ display_name: '共享', key_code: 'ABC' })).toBe('共享 · ABC')
		expect(enrollmentKeyName({ key_code: 'ABC' })).toBe('ABC')
		expect(enrollmentKeyName(null)).toBe('未命名密钥')
	})
	it('renders empty values as a dash', () => {
		expect(valueOrDash('')).toBe('—')
		expect(valueOrDash(null)).toBe('—')
		expect(valueOrDash(0)).toBe('0')
	})
})
