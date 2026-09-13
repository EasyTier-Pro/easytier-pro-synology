// Payloads of the daemon's local API. The daemon is the only producer and it
// ships with the interface, so these describe its current answers; fields that
// the daemon may extend are kept optional so a newer daemon cannot break a page.

export interface Status {
	enabled?: boolean
	console_url?: string
	allow_insecure_console?: boolean
	config_server?: string
	install_dir?: string
	running?: boolean
	token_present?: boolean
	core_installed?: boolean
	cli_installed?: boolean
	core_version?: string
	cli_version?: string
	machine_id?: string
	workspace_id?: string
	/** Whether the core may create a virtual interface (CAP_NET_ADMIN). */
	tun_capable?: boolean
	/** Whether the core may bind its sockets to an interface (CAP_NET_RAW). */
	bind_capable?: boolean
	/** Whether the Console already holds the mode this device needs. */
	mode_synced?: boolean
	/** The release the Console last offered; absent until it is known. */
	latest_version?: string
	/** Whether the installed core is older than that release. */
	core_update_available?: boolean
}

export interface AuthStatus {
	logged_in?: boolean
	[key: string]: unknown
}

/** The runtime-download state machine the daemon reports. */
export interface DownloadStatus {
	state?: string
	phase?: string
	percent?: number | string
	message?: string
	version?: string
}

/** One background connection-change operation. */
export interface Operation {
	operation_id?: string
	state?: string
	phase?: string
	message?: string
	needs_download?: boolean
}

export interface Network {
	id?: string
	name?: string
	network_name?: string
	ipv4_cidr?: string
}

/** The device's own membership of one network. */
export interface Membership {
	id?: string
	ipv4_addr?: string
	node_ipv4?: string
	virtual_ip?: string
	network_name?: string
	[key: string]: unknown
}

export interface Machine {
	id?: string
	machine_id?: string
	networks?: Membership[]
}

export interface NetworksPayload {
	workspace_id?: string
	machine_id?: string
	machine?: Machine
	networks?: Network[]
	/** False while the device is still registering with the Console. */
	enrolled?: boolean
}

export interface Node {
	id?: string
	machine_id?: string
	hostname?: string
	display_name?: string
	ipv4_addr?: string
	dns_name?: string
	os?: string
	os_version?: string
	version?: string
	status?: string
	lifecycle_state?: string
	runtime_health_status?: string
	connectivity_state?: string
	last_seen?: string
	is_exit_node?: boolean
	is_subnet_router?: boolean
}

export interface NodesPayload {
	nodes?: Node[]
}

/**
 * The runtime summary. `node` and `peers` arrive wrapped differently depending
 * on the core version, which `localIPv4`/`peerEntries` normalise.
 */
export interface LocalSummary {
	node?: unknown
	peers?: unknown
	interfaces?: unknown
	[key: string]: unknown
}

export interface EnrollmentKey {
	id?: string
	display_name?: string
	key_code?: string
	used_count?: number | string
	pre_approved?: boolean
	reusable?: boolean
	revoked?: boolean
}

export interface EnrollmentOptions {
	workspace_id?: string
	machine_id?: string
	existing_device?: boolean
	local_connection?: boolean
	current_key?: EnrollmentKey | null
	current_key_unavailable?: boolean
	reusable_keys?: EnrollmentKey[]
}

export interface Workspace {
	id?: string
	slug?: string
	name?: string
	role?: string
	[key: string]: unknown
}

export interface AccountPayload {
	tenants?: Workspace[]
	user?: unknown
	[key: string]: unknown
}

export interface AuthStartPayload {
	user_code?: string
	verification_uri?: string
	verification_uri_complete?: string
	interval?: number | string
	device_code?: string
	[key: string]: unknown
}

export interface AuthPollPayload {
	authenticated?: boolean
	retry_after?: number | string
	[key: string]: unknown
}

export interface LogsPayload {
	lines?: number
	logs?: string
}

/** The everyday settings the settings page reads and writes. */
export interface Settings {
	enabled: boolean
	console_url: string
	allow_insecure_console: boolean
	config_server: string
}

/** The daemon's own envelope, which also flags its failures. */
export interface Envelope {
	ok?: boolean
	error?: string
	message?: string
}
