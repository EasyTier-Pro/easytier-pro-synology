// The enrolment choices offered for one workspace, derived from what the
// Console reports about this device. Keeping this separate from the dialog makes
// the list order and wording testable on their own.

import { enrollmentKeyName } from './format'
import type { EnrollmentOptions } from '@/api/types'

export interface EnrollmentChoice {
	mode: string
	keyID: string
	label: string
	hint: string
}

/** buildEnrollmentChoices lists the ways this device can be enrolled. */
export function buildEnrollmentChoices(options?: EnrollmentOptions | null): EnrollmentChoice[] {
	const choices: EnrollmentChoice[] = []
	const source = options || {}

	if (source.local_connection) {
		choices.push({
			mode: 'local',
			keyID: '',
			label: '使用本机已保存的密钥',
			hint: '沿用本机之前保存的注册密钥完成设置。',
		})
	}
	if (source.existing_device && source.current_key) {
		choices.push({
			mode: 'recover',
			keyID: source.current_key.id || '',
			label: `恢复当前设备密钥：${enrollmentKeyName(source.current_key)}`,
			hint: '找回这台设备之前在 Console 上使用的密钥。',
		})
	}

	// Pre-approved keys come first: they need no administrator, so they are the
	// ones most likely to work straight away.
	const reusable = [ ...(source.reusable_keys || []) ].sort(
		(left, right) => Number(Boolean(right.pre_approved)) - Number(Boolean(left.pre_approved)),
	)
	for (const key of reusable) {
		const used = Math.max(0, Number(key.used_count || 0))
		const approval = key.pre_approved ? '' : '，需要管理员审批'
		choices.push({
			mode: 'existing',
			keyID: key.id || '',
			label: `使用已有共享密钥：${enrollmentKeyName(key)}（已使用 ${used} 次${approval}）`,
			hint: '多台设备可以共用同一个密钥。',
		})
	}

	choices.push({
		mode: 'shared',
		keyID: '',
		label: '新建共享密钥',
		hint: '创建一个可重复使用的密钥，之后其它设备也可以用它加入。',
	})
	choices.push({
		mode: 'dedicated',
		keyID: '',
		label: '新建专用密钥',
		hint: '创建一个只属于这台设备的密钥，撤销时只影响本机。',
	})

	return choices
}
