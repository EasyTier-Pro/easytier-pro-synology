// Test environment setup.
//
// happy-dom does not provide localStorage here, and an in-memory one is enough:
// the only thing the interface stores is the operator's appearance choice, and
// the tests that check it need to control it exactly.

class MemoryStorage implements Storage {
	private items = new Map<string, string>()

	get length(): number {
		return this.items.size
	}

	clear(): void {
		this.items.clear()
	}

	getItem(key: string): string | null {
		return this.items.has(key) ? this.items.get(key)! : null
	}

	key(index: number): string | null {
		return [ ...this.items.keys() ][index] ?? null
	}

	removeItem(key: string): void {
		this.items.delete(key)
	}

	setItem(key: string, value: string): void {
		this.items.set(key, String(value))
	}
}

if (!globalThis.localStorage) {
	const storage = new MemoryStorage()
	Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true, writable: true })
	Object.defineProperty(window, 'localStorage', { value: storage, configurable: true, writable: true })
}
