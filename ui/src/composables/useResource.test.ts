import { describe, expect, it, vi } from 'vitest'
import { useResource } from './useResource'

// The defect these guard: a region that reloads used to blank itself, and a slow
// first request could overwrite the result of a newer one. Both would show the
// operator stale or empty content for no reason.

describe('useResource', () => {
	it('keeps the current value visible while a reload is in flight', async () => {
		let answer: (value: string) => void = () => {}
		const loader = vi.fn()
			.mockResolvedValueOnce('first')
			.mockImplementationOnce(() => new Promise<string>((resolve) => { answer = resolve }))
		const region = useResource(loader)

		await region.reload()
		expect(region.data.value).toBe('first')

		const pending = region.reload()
		expect(region.loading.value).toBe(true)
		// The old value survives the reload, so the caller can keep rendering it.
		expect(region.data.value).toBe('first')

		answer('second')
		await pending
		expect(region.data.value).toBe('second')
	})

	it('keeps the last good value when a reload fails', async () => {
		const loader = vi.fn()
			.mockResolvedValueOnce('good')
			.mockRejectedValueOnce(new Error('上游不可用'))
		const region = useResource(loader)

		await region.reload()
		await region.reload()

		expect(region.data.value).toBe('good')
		expect(region.error.value).toBeInstanceOf(Error)
	})

	it('lets a newer attempt win over a slower earlier one', async () => {
		let slow: (value: string) => void = () => {}
		const loader = vi.fn()
			.mockImplementationOnce(() => new Promise<string>((resolve) => { slow = resolve }))
			.mockResolvedValueOnce('new')
		const region = useResource(loader)

		const first = region.reload()
		const second = region.reload()
		await second

		expect(region.data.value).toBe('new')
		// The earlier answer arrives last and must be discarded.
		slow('stale')
		await first
		expect(region.data.value).toBe('new')
	})

	it('reports whether any attempt has finished', async () => {
		const region = useResource(() => Promise.resolve('value'))
		expect(region.settled.value).toBe(false)
		await region.reload()
		expect(region.settled.value).toBe(true)
	})
})
