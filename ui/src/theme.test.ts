import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The appearance choice has to survive the page, and the background painted
// before the bundle arrives has to agree with it - otherwise a dark interface
// opens on a white canvas and the cards appear out of it one by one.

let listeners: Array<(event: { matches: boolean }) => void> = []
let systemDark = false

function installMatchMedia(): void {
	listeners = []
	window.matchMedia = ((query: string) => ({
		matches: query.includes('dark') ? systemDark : false,
		media: query,
		addEventListener: (_type: string, listener: (event: { matches: boolean }) => void) => {
			listeners.push(listener)
		},
		removeEventListener: () => {},
		addListener: () => {},
		removeListener: () => {},
		dispatchEvent: () => false,
		onchange: null,
	})) as unknown as typeof window.matchMedia
}

/** setSystemDark flips the system preference the way the browser would. */
function setSystemDark(value: boolean): void {
	systemDark = value
	for (const listener of listeners) {
		listener({ matches: value })
	}
}

/** loadTheme imports the module fresh, as a page load would. */
async function loadTheme() {
	vi.resetModules()
	return import('./theme')
}

describe('appearance', () => {
	beforeEach(() => {
		systemDark = false
		installMatchMedia()
		localStorage.clear()
		document.documentElement.removeAttribute('data-theme')
	})

	afterEach(() => {
		localStorage.clear()
		document.documentElement.removeAttribute('data-theme')
	})

	it('follows the system until a choice is made', async () => {
		const theme = await loadTheme()
		expect(theme.themeMode.value).toBe('system')
		expect(theme.isDark.value).toBe(false)

		setSystemDark(true)
		expect(theme.isDark.value).toBe(true)
		expect(theme.themeMode.value).toBe('system')
	})

	it('uses the chosen appearance whatever the system says', async () => {
		const theme = await loadTheme()
		setSystemDark(true)

		theme.setThemeMode('light')
		expect(theme.isDark.value).toBe(false)
		expect(theme.themeMode.value).toBe('light')

		setSystemDark(false)
		theme.setThemeMode('dark')
		expect(theme.isDark.value).toBe(true)
	})

	it('remembers the choice for the next visit', async () => {
		const first = await loadTheme()
		first.setThemeMode('dark')
		expect(localStorage.getItem('easytier-pro-theme')).toBe('dark')

		// A page load reads it back without asking the system again.
		const second = await loadTheme()
		expect(second.themeMode.value).toBe('dark')
		expect(second.isDark.value).toBe(true)
	})

	it('returns to the system when the choice is given back', async () => {
		const theme = await loadTheme()
		theme.setThemeMode('dark')
		theme.setThemeMode('system')

		expect(localStorage.getItem('easytier-pro-theme')).toBeNull()
		expect(theme.themeMode.value).toBe('system')
	})

	it('ignores a stored value it does not recognise', async () => {
		localStorage.setItem('easytier-pro-theme', 'sepia')
		const theme = await loadTheme()
		expect(theme.themeMode.value).toBe('system')
	})

	// index.html paints the background from this attribute before the bundle
	// runs, so it has to be kept in step - including in the system case, where
	// leaving it unset is what lets the media query decide.
	it('keeps the root attribute index.html paints from in step', async () => {
		const theme = await loadTheme()

		theme.setThemeMode('dark')
		expect(document.documentElement.getAttribute('data-theme')).toBe('dark')

		theme.setThemeMode('light')
		expect(document.documentElement.getAttribute('data-theme')).toBe('light')

		theme.setThemeMode('system')
		expect(document.documentElement.hasAttribute('data-theme')).toBe(false)
	})

	it('repaints the root background for a stored choice on load', async () => {
		localStorage.setItem('easytier-pro-theme', 'dark')
		const theme = await loadTheme()

		// applyStoredTheme is what main.ts calls; the attribute must already be
		// right for the saved choice.
		theme.applyStoredTheme()
		expect(document.documentElement.getAttribute('data-theme')).toBe('dark')
		expect(theme.isDark.value).toBe(true)
	})
})
