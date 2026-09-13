// The overview page polls during long-running steps. It regressed once because
// the "stop polling" flag was only ever set on unmount, so the second visit to
// the page could not poll at all and its progress dialogs never closed.
import test from 'node:test';
import assert from 'node:assert/strict';

// Minimal timer host: the module schedules through window.setTimeout.
globalThis.window = {
	setTimeout: (fn, ms) => setTimeout(fn, ms),
	clearTimeout: (id) => clearTimeout(id),
};

const { createTimerScope } = await import('./timers.js');

const settle = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

test('scheduled work runs', async () => {
	const scope = createTimerScope();
	let ran = false;
	scope.after(1, () => { ran = true; });
	await settle(20);
	assert.equal(ran, true);
});

test('invalidate abandons pending work', async () => {
	const scope = createTimerScope();
	let ran = false;
	scope.after(30, () => { ran = true; });
	scope.invalidate();
	await settle(60);
	assert.equal(ran, false);
});

// The regression: after invalidate, a newly scheduled timer must still run.
test('scheduling works again after invalidate', async () => {
	const scope = createTimerScope();
	scope.after(10, () => {});
	scope.invalidate();

	let ran = false;
	scope.after(1, () => { ran = true; });
	await settle(30);
	assert.equal(ran, true, 'a timer created after invalidate never ran');
});

test('polling can be driven across several mounts', async () => {
	const scope = createTimerScope();
	const ticks = [];
	const poll = () => scope.after(1, () => { ticks.push(1); poll(); });

	for (let mount = 0; mount < 3; mount++) {
		poll();
		await settle(15);
		scope.invalidate();
	}
	assert.ok(ticks.length >= 3, `expected ticks on every mount, got ${ticks.length}`);
});
