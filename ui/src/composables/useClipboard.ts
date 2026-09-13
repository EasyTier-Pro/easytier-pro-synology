import { notify } from '@/naive'

/**
 * copyText writes text to the clipboard and reports the outcome.
 *
 * The clipboard API is unavailable outside a secure context, which a NAS
 * reached over plain HTTP is, so a failure is expected there and is reported as
 * something the operator can work around by selecting the text themselves.
 */
export async function copyText(text: string): Promise<void> {
	try {
		await navigator.clipboard.writeText(text)
		notify('命令已复制。', 'success')
	} catch {
		notify('复制失败，请手动选择命令文本。', 'error')
	}
}
