import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createTimerScope } from './timers'

// The scope exists so that a view can poll again after it is remounted: a
// one-way stop flag would leave a later mount silent.

describe('timer scope', () => {
	beforeEach(() => {
		vi.useFakeTimers()
	})

	afterEach(() => {
		vi.useRealTimers()
	})

	it('runs scheduled work', () => {
		const scope = createTimerScope()
		const ran = vi.fn()
		scope.after(10, ran)

		vi.advanceTimersByTime(9)
		expect(ran).not.toHaveBeenCalled()
		vi.advanceTimersByTime(1)
		expect(ran).toHaveBeenCalledTimes(1)
	})

	it('abandons pending work when invalidated', () => {
		const scope = createTimerScope()
		const ran = vi.fn()
		scope.after(10, ran)
		scope.invalidate()

		vi.advanceTimersByTime(100)
		expect(ran).not.toHaveBeenCalled()
	})

	it('schedules again after an invalidate', () => {
		const scope = createTimerScope()
		const first = vi.fn()
		scope.after(10, first)
		scope.invalidate()

		const second = vi.fn()
		scope.after(10, second)
		vi.advanceTimersByTime(10)

		expect(first).not.toHaveBeenCalled()
		expect(second).toHaveBeenCalledTimes(1)
	})

	it('keeps polling across several mounts', () => {
		const scope = createTimerScope()
		const ticks: number[] = []

		const schedule = (): void => {
			scope.after(5, () => {
				ticks.push(ticks.length + 1)
			})
		}

		// First mount.
		schedule()
		vi.advanceTimersByTime(5)
		expect(ticks).toHaveLength(1)

		// Leaving the view and coming back must poll again.
		scope.invalidate()
		schedule()
		vi.advanceTimersByTime(5)
		expect(ticks).toHaveLength(2)

		scope.invalidate()
		schedule()
		vi.advanceTimersByTime(5)
		expect(ticks).toHaveLength(3)
	})
})
