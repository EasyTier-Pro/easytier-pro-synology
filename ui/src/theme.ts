// Which appearance the interface uses.
//
// The choice is the operator's, so it is stored and it outlives the page: the
// same value is read by the inline script in index.html before the first paint,
// which is why the key and the accepted values live here in one place.

import { computed, ref } from 'vue'

export type ThemeMode = 'system' | 'light' | 'dark'

/** The localStorage key index.html reads before the bundle is loaded. */
export const THEME_KEY = 'easytier-pro-theme'

const prefersDark = window.matchMedia?.('(prefers-color-scheme: dark)')

/** storedMode reads the saved appearance, falling back to the system. */
export function storedMode(): ThemeMode {
	try {
		const value = localStorage.getItem(THEME_KEY)
		return value === 'light' || value === 'dark' ? value : 'system'
	} catch {
		return 'system'
	}
}

export const themeMode = ref<ThemeMode>(storedMode())

const systemDark = ref(Boolean(prefersDark?.matches))

prefersDark?.addEventListener('change', (event) => {
	systemDark.value = event.matches
})

/** Whether the dark theme is in use, by choice or by following the system. */
export const isDark = computed(() => themeMode.value === 'dark'
	|| (themeMode.value === 'system' && systemDark.value))

/**
 * setThemeMode applies a choice, remembers it, and repaints the page background.
 *
 * The background is set on the root element rather than by the interface, so the
 * part of the window the cards do not cover follows the theme too.
 */
export function setThemeMode(mode: ThemeMode): void {
	themeMode.value = mode
	try {
		if (mode === 'system') {
			localStorage.removeItem(THEME_KEY)
		} else {
			localStorage.setItem(THEME_KEY, mode)
		}
	} catch {
		// Storage unavailable: the choice holds for this page only.
	}
	const root = document.documentElement
	if (mode === 'system') {
		root.removeAttribute('data-theme')
	} else {
		root.setAttribute('data-theme', mode)
	}
}

/** applyStoredTheme repaints the background for the saved choice. */
export function applyStoredTheme(): void {
	const mode = storedMode()
	themeMode.value = mode
	const root = document.documentElement
	if (mode === 'system') {
		root.removeAttribute('data-theme')
	} else {
		root.setAttribute('data-theme', mode)
	}
}
