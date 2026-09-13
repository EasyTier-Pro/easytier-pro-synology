// Naive UI's discrete API: the toasts and dialogs are raised from async
// callbacks (polls, operation results) rather than from a component's setup, so
// they are created once here instead of being injected through providers.

import { createDiscreteApi, darkTheme, type ConfigProviderProps } from 'naive-ui'
import { computed } from 'vue'
import { isDark } from './theme'

export { isDark, setThemeMode, themeMode } from './theme'

const configProviderProps = computed<ConfigProviderProps>(() => ({
	theme: isDark.value ? darkTheme : null,
}))

const { message, dialog, notification } = createDiscreteApi(
	[ 'message', 'dialog', 'notification' ],
	{ configProviderProps },
)

export { message, dialog, notification }

/** notify shows a transient message, matching the previous interface's levels. */
export function notify(text: string, level: 'info' | 'success' | 'warning' | 'error' = 'info'): void {
	message[level](text, { duration: level === 'error' ? 7000 : 4000 })
}

export interface ConfirmOptions {
	title: string
	body: string
	positiveText: string
	/** Destructive actions are marked, so they are harder to confirm by reflex. */
	negative?: boolean
}

/**
 * confirmDestructive asks before an action that removes something or
 * interrupts connectivity. It resolves true only when the operator agrees.
 */
export function confirmDestructive(options: ConfirmOptions): Promise<boolean> {
	return new Promise((resolve) => {
		dialog.warning({
			title: options.title,
			content: options.body,
			positiveText: options.positiveText,
			negativeText: '取消',
			onPositiveClick: () => resolve(true),
			onNegativeClick: () => resolve(false),
			onClose: () => resolve(false),
			onMaskClick: () => resolve(false),
		})
	})
}
