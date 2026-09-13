import { h, ref } from 'vue'
import { dialog, notify } from '@/naive'
import { api } from '@/api/client'
import { messageOf } from '@/api/errors'
import { operationMessage } from '@/utils/phases'
import type { Operation } from '@/api/types'

// Background connection changes are executed by the daemon and reported through
// one operation record, so progress is a poll rather than a response. The
// dialog stays open for the whole change, because the change keeps running even
// if the operator leaves the page.

const POLL_INTERVAL = 1200
const POLL_RETRY_INTERVAL = 2000
const MAX_POLL_FAILURES = 6

export interface PollScope {
	after: (delay: number, callback: () => void) => number
}

export interface OperationHandlers {
	progress?: (operation: Operation) => void
	completed?: (operation: Operation) => void
	failed: (operation: Operation) => void
	/** Called on every failed status query; silence is a valid choice. */
	error?: (error: unknown, failures: number) => void
}

/**
 * pollOperation follows one operation to its end.
 *
 * A failed status query is not an operation failure: the change continues in the
 * daemon, so the query is retried a few times before the operation is reported
 * as unreadable.
 */
export function pollOperation(
	operationID: string,
	handlers: OperationHandlers,
	scope: PollScope,
	initialDelay = POLL_INTERVAL,
): void {
	let failures = 0

	const tick = (): void => {
		api.operationStatus(operationID).then((operation) => {
			failures = 0
			switch (operation.state) {
				case 'queued':
				case 'running':
					if (handlers.progress) {
						handlers.progress(operation)
					}
					scope.after(POLL_INTERVAL, tick)
					return
				case 'failed':
					handlers.failed(operation)
					return
				case 'completed':
					if (handlers.completed) {
						handlers.completed(operation)
					}
					return
				default:
					handlers.failed({ message: '操作返回了未知状态。' })
			}
		}).catch((error) => {
			failures += 1
			if (handlers.error) {
				handlers.error(error, failures)
			}
			if (failures < MAX_POLL_FAILURES) {
				scope.after(POLL_RETRY_INTERVAL, tick)
				return
			}
			handlers.failed({ message: `无法查询操作进度：${messageOf(error)}` })
		})
	}

	scope.after(initialDelay, tick)
}

export interface OperationOutcome {
	completed: (operation: Operation) => void
	failed?: (operation: Operation) => void
}

/**
 * showOperationProgress runs one operation behind a modal that reports its
 * phase. The modal cannot be dismissed: the operation is already committed, and
 * closing it would only hide the outcome.
 */
export function showOperationProgress(
	title: string,
	operationID: string,
	outcome: OperationOutcome,
	scope: PollScope,
): void {
	const phase = ref('正在排队执行本机设置变更…')

	const instance = dialog.info({
		title,
		content: () => h('div', {}, [
			h('p', { class: 'etp-paragraph' }, '请稍候，正在完成本机设置变更。'),
			h('p', { class: 'etp-paragraph etp-muted' }, phase.value),
			h('p', { class: 'etp-paragraph etp-muted' }, '操作在后台继续执行，可以暂时离开本页面。'),
		]),
		closable: false,
		maskClosable: false,
		closeOnEsc: false,
		onPositiveClick: () => false,
	})

	pollOperation(operationID, {
		progress: (operation) => {
			phase.value = operationMessage(operation)
		},
		completed: (operation) => {
			instance.destroy()
			outcome.completed(operation)
		},
		failed: (operation) => {
			instance.destroy()
			// The operation is over and the dialog is gone, so the failure has to
			// be said out loud: it covers both a change the daemon refused and a
			// progress query that could not be read to the end.
			notify(operation.message || '操作失败，请重试。', 'error')
			if (outcome.failed) {
				outcome.failed(operation)
			}
		},
		error: () => {},
	}, scope)
}
