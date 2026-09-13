import { describe, expect, it } from 'vitest'
import { buildEnrollmentChoices } from './enrollment'

describe('enrollment choices', () => {
	it('offers the two ways of creating a key when nothing else applies', () => {
		const choices = buildEnrollmentChoices({})
		expect(choices.map((choice) => choice.mode)).toEqual([ 'shared', 'dedicated' ])
		expect(choices[0].label).toBe('新建共享密钥')
		expect(choices[1].label).toBe('新建专用密钥')
	})

	it('puts reusable keys before the choices that create one', () => {
		const choices = buildEnrollmentChoices({
			reusable_keys: [ { id: 'k1', key_code: 'ABC' } ],
		})
		expect(choices.map((choice) => choice.mode)).toEqual([ 'existing', 'shared', 'dedicated' ])
	})

	it('orders pre-approved keys first, so the ones that need no administrator lead', () => {
		const choices = buildEnrollmentChoices({
			reusable_keys: [
				{ id: 'needs-approval', key_code: 'AAA', pre_approved: false },
				{ id: 'approved', key_code: 'BBB', pre_approved: true },
			],
		})
		const existing = choices.filter((choice) => choice.mode === 'existing')
		expect(existing.map((choice) => choice.keyID)).toEqual([ 'approved', 'needs-approval' ])
		// The unapproved key says so where it is offered.
		expect(existing[0].label).not.toContain('需要管理员审批')
		expect(existing[1].label).toContain('需要管理员审批')
	})

	it('reports how often a shared key has been used', () => {
		const choices = buildEnrollmentChoices({
			reusable_keys: [ { id: 'k1', key_code: 'ABC', used_count: 3 } ],
		})
		expect(choices[0].label).toContain('已使用 3 次')
	})

	it('offers the saved key and the device key when the Console reports them', () => {
		const choices = buildEnrollmentChoices({
			local_connection: true,
			existing_device: true,
			current_key: { id: 'current', display_name: '旧密钥' },
		})
		expect(choices.map((choice) => choice.mode)).toEqual([ 'local', 'recover', 'shared', 'dedicated' ])
		expect(choices[1].keyID).toBe('current')
		expect(choices[1].label).toContain('旧密钥')
	})

	it('does not offer the device key when the Console says it is not there', () => {
		const choices = buildEnrollmentChoices({ existing_device: false, current_key: { id: 'current' } })
		expect(choices.some((choice) => choice.mode === 'recover')).toBe(false)
	})
})
