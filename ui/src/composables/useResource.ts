import { ref, shallowRef, type Ref } from 'vue'

// One independently loadable region of a page.
//
// Pages are built from several resources (status, summary, networks), and each
// one can fail or be reloaded on its own. Keeping them separate is what lets an
// action refresh only the region it changed: the rest of the page keeps the data
// and the DOM it already had, so nothing blanks out and the operator does not
// lose their scroll position or an expanded section.
export interface Resource<T> {
	/** The last successful value, kept across a reload. */
	data: Ref<T | null>
	/** The failure of the most recent attempt, cleared when one succeeds. */
	error: Ref<unknown | null>
	/** Whether an attempt is in flight. */
	loading: Ref<boolean>
	/** Whether any attempt has finished, successfully or not. */
	settled: Ref<boolean>
	/** Reloads the region, keeping the current value visible meanwhile. */
	reload: () => Promise<void>
	/** Replaces the value without contacting the daemon. */
	set: (value: T | null) => void
}

export interface ResourceOptions {
	/** Runs after a successful load, for derived bookkeeping. */
	onSuccess?: (value: unknown) => void
}

/**
 * useResource loads one region and lets it be reloaded on its own.
 *
 * A reload never clears the current value: the caller can keep rendering it
 * while the new one is on the way, which is the whole point of splitting a page
 * into regions. Only the very first attempt has nothing to show, and that is
 * what `settled` distinguishes.
 *
 * Replies are matched to the attempt that asked for them. A reload started
 * later always wins, so a slow first request cannot overwrite a newer result.
 */
export function useResource<T>(loader: () => Promise<T>, options: ResourceOptions = {}): Resource<T> {
	const data = shallowRef<T | null>(null)
	const error = ref<unknown | null>(null)
	const loading = ref(false)
	const settled = ref(false)
	let attempt = 0

	async function reload(): Promise<void> {
		const current = ++attempt
		loading.value = true
		try {
			const value = await loader()
			if (current !== attempt) {
				return
			}
			data.value = value
			error.value = null
			if (options.onSuccess) {
				options.onSuccess(value)
			}
		} catch (failure) {
			if (current !== attempt) {
				return
			}
			// The previous value is deliberately kept: a region that fails to
			// refresh is still better than one that empties itself.
			error.value = failure
		} finally {
			if (current === attempt) {
				loading.value = false
				settled.value = true
			}
		}
	}

	function set(value: T | null): void {
		data.value = value
		error.value = null
		settled.value = true
	}

	return { data, error, loading, settled, reload, set }
}

/** all loads several regions together, the way a page does on entry. */
export function loadAll(resources: Array<{ reload: () => Promise<void> }>): Promise<void> {
	return Promise.all(resources.map((resource) => resource.reload())).then(() => undefined)
}
