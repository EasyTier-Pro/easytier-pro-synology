// The runtime summary arrives wrapped differently depending on the core
// version: a plain array, a `{result: [...]}` envelope, or a map of per-instance
// results. These helpers normalise it so the pages can stay simple.

interface ResultEnvelope {
	result?: unknown[]
}

function isEnvelope(value: unknown): value is ResultEnvelope {
	return Boolean(value) && typeof value === 'object' && Array.isArray((value as ResultEnvelope).result)
}

/** peerEntries flattens every shape the peer list is reported in. */
export function peerEntries(value: unknown): unknown[] {
	if (!value) {
		return []
	}
	if (Array.isArray(value)) {
		return value.flatMap((item) => (item && isEnvelope(item) ? item.result! : [ item ]))
	}
	if (isEnvelope(value)) {
		return value.result!
	}
	if (typeof value === 'object') {
		return Object.values(value as Record<string, unknown>)
	}
	return []
}

/** peerCount is how many peers the runtime reports. */
export function peerCount(value: unknown): number {
	return peerEntries(value).length
}

/** interfaceNames lists the tunnel interfaces the runtime created. */
export function interfaceNames(value: unknown): string[] {
	if (!Array.isArray(value)) {
		return []
	}
	return value.filter((item): item is string => typeof item === 'string' && item !== '')
}

function matchIPv4(entry: unknown): string | null {
	if (!entry || typeof entry !== 'object') {
		return null
	}
	const record = entry as { ipv4_addr?: string; ipv4Addr?: string }
	return record.ipv4_addr || record.ipv4Addr || null
}

/**
 * localIPv4 finds this machine's virtual address: first on the node entry
 * itself, then inside its per-instance results, and finally by matching a peer.
 */
export function localIPv4(summary?: { node?: unknown; peers?: unknown } | null): string {
	const node = summary && summary.node
	if (node && typeof node === 'object') {
		const direct = matchIPv4(node)
		if (direct) {
			return direct
		}
		const items = Array.isArray(node)
			? node
			: (Array.isArray((node as ResultEnvelope).result) ? (node as ResultEnvelope).result! : [])
		for (const item of items) {
			// Multi-instance output wraps each instance in a result object.
			const found = matchIPv4(item) || matchIPv4((item as ResultEnvelope | undefined)?.result)
			if (found) {
				return found
			}
		}
	}
	for (const peer of peerEntries(summary && summary.peers)) {
		const found = matchIPv4(peer)
		if (found) {
			return found
		}
	}
	return ''
}
