// The state the pages share.
//
// Several pages read the same few resources, and a page that refetches them from
// scratch shows a loading placeholder every time it is opened. Holding them here
// means a page renders immediately from what was last read and refreshes in the
// background, so moving between pages looks like switching a view rather than
// reloading a document.

import { ref } from 'vue'
import { api } from '@/api/client'
import { ApiError } from '@/api/errors'
import { useResource, type Resource } from '@/composables/useResource'
import type {
	AuthStatus,
	DownloadStatus,
	LocalSummary,
	NetworksPayload,
	Status,
} from '@/api/types'

/** The resources every page may need, created once for the session. */
export interface Resources {
	status: Resource<Status>
	auth: Resource<AuthStatus>
	download: Resource<DownloadStatus>
	summary: Resource<LocalSummary>
	networks: Resource<NetworksPayload>
}

function create(): Resources {
	return {
		status: useResource(() => api.status()),
		auth: useResource(() => api.authStatus()),
		download: useResource(() => api.downloadStatus()),
		// The summary always fails while the core is down, and that state is already
		// reported by `status`, so a missing core is not an error worth showing.
		summary: useResource(() => api.localSummary().catch((error) => {
			if (error instanceof ApiError && error.code === 'service_not_running') {
				return {}
			}
			throw error
		})),
		networks: useResource(() => api.networks()),
	}
}

/**
 * How many log lines the logs page asks for. It is a preference rather than page
 * state, so leaving the page and coming back keeps the operator's choice.
 */
export const logLineCount = ref(200)

let resources = create()

/** resources_ returns the session's shared resources. */
export function shared(): Resources {
	return resources
}

/**
 * refreshStatus reads the small always-relevant resources together. Every page
 * needs the device status, so it is loaded once here rather than per page.
 */
export async function refreshStatus(): Promise<void> {
	const current = resources
	await Promise.all([ current.status.reload(), current.auth.reload(), current.download.reload() ])
}

/**
 * resetShared drops everything that was read. Tests call this between cases so
 * one case cannot render from another's data.
 */
export function resetShared(): void {
	resources = create()
}
