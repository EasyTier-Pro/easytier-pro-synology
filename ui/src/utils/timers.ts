// Timer scope for a polled view.
//
// A view that polls has to stop polling when it is left, but it must also be
// able to poll again after it is remounted - the router unmounts and remounts
// views, so a flag that is only ever set on unmount would permanently stop the
// polling the second time the view is shown.
//
// Timers are keyed to the mount they were created in. invalidate() marks the
// current mount as finished, so pending work is dropped, while timers created
// afterwards belong to the new mount and run normally.

export interface TimerScope {
	after: (delay: number, callback: () => void) => number
	invalidate: () => void
}

export function createTimerScope(): TimerScope {
	let generation = 0
	const timers = new Set<number>()

	function after(delay: number, callback: () => void): number {
		const created = generation
		const id = window.setTimeout(() => {
			timers.delete(id)
			if (created === generation) {
				callback()
			}
		}, delay)
		timers.add(id)
		return id
	}

	function invalidate(): void {
		generation += 1
		for (const id of timers) {
			window.clearTimeout(id)
		}
		timers.clear()
	}

	return { after, invalidate }
}
