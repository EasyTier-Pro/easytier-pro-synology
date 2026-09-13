// Timer scope for a polled page.
//
// A page that polls has to stop polling when it is left, but it must also be
// able to poll again after it is remounted - the router unmounts and remounts
// pages of the same module, so a flag that is only ever set on unmount would
// permanently stop the page from polling the second time it is shown.

/**
 * createTimerScope returns a timer scope: `after` schedules work and
 * `invalidate` abandons everything scheduled so far.
 *
 * Timers are keyed to the mount they were created in. invalidate() marks the
 * current mount as finished, so pending work is dropped, while timers created
 * afterwards belong to the new mount and run normally.
 */
export function createTimerScope() {
	let generation = 0;
	const timers = new Set();

	function after(delay, callback) {
		const created = generation;
		const id = window.setTimeout(() => {
			timers.delete(id);
			if (created === generation) {
				callback();
			}
		}, delay);
		timers.add(id);
		return id;
	}

	function invalidate() {
		generation += 1;
		for (const id of timers) {
			window.clearTimeout(id);
		}
		timers.clear();
	}

	return { after, invalidate };
}
