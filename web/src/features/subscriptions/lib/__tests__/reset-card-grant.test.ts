/*
Copyright (C) 2023-2026 xingguangcuican6666

This file is part of new-api.

new-api is free software: you can redistribute it and/or modify it under the
terms of the GNU Affero General Public License as published by the Free Software
Foundation, either version 3 of the License, or (at your option) any later
version.

new-api is distributed in the hope that it will be useful, but WITHOUT ANY
WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License along
with new-api. If not, see <https://www.gnu.org/licenses/>.

Original project: https://github.com/QuantumNous/new-api
*/
import { describe, expect, test } from 'vitest'

import {
  EMPTY_RESET_CARD_GRANT_DRAFT,
  downloadResetCardKeys,
  isResetCardGrantDraftComplete,
  recipientFilterPayload,
  resetCardGrantKeys,
  resetCardGrantPayload,
  type ResetCardGrantDraft,
} from '../reset-card-grant'

function draft(overrides: Partial<ResetCardGrantDraft> = {}): ResetCardGrantDraft {
  return {
    ...EMPTY_RESET_CARD_GRANT_DRAFT,
    scope: {
      advance_reset_time: true,
      reset_amount_used: true,
      reset_cycle_limits: false,
      reset_window_limits: false,
    },
    ...overrides,
  }
}

describe('reset card grant payload', () => {
  test('users mode sends the id list and no filter', () => {
    const payload = resetCardGrantPayload(
      draft({ recipientMode: 'users', userIds: [3, 9] })
    )
    expect(payload.recipient_mode).toBe('users')
    expect(payload.user_ids).toEqual([3, 9])
    expect(payload.filter).toBeUndefined()
  })

  test('filter mode sends the conditions and no id list', () => {
    const payload = resetCardGrantPayload(
      draft({
        recipientMode: 'filter',
        userIds: [3],
        filters: {
          keyword: ' vip ',
          group: 'pro',
          role: '10',
          status: '1',
        },
      })
    )
    expect(payload.recipient_mode).toBe('filter')
    expect(payload.user_ids).toBeUndefined()
    expect(payload.filter).toEqual({
      keyword: 'vip',
      group: 'pro',
      role: 10,
      status: 1,
    })
  })

  test('all mode sends neither ids nor a filter', () => {
    const payload = resetCardGrantPayload(
      draft({
        recipientMode: 'all',
        userIds: [3],
        filters: { keyword: 'x', group: 'g', role: '1', status: '1' },
      })
    )
    expect(payload.recipient_mode).toBe('all')
    expect(payload.user_ids).toBeUndefined()
    expect(payload.filter).toBeUndefined()
  })

  test('codes mode sends neither ids nor a filter', () => {
    const payload = resetCardGrantPayload(
      draft({ recipientMode: 'codes', userIds: [3], count: 25 })
    )
    expect(payload.recipient_mode).toBe('codes')
    expect(payload.user_ids).toBeUndefined()
    expect(payload.filter).toBeUndefined()
    expect(payload.count).toBe(25)
  })

  test('a stale plan id is dropped when the scope is not plan', () => {
    const payload = resetCardGrantPayload(
      draft({ targetScope: 'all', planId: '12' })
    )
    expect(payload.plan_id).toBeUndefined()
  })

  test('empty filter conditions are dropped so server defaults apply', () => {
    expect(recipientFilterPayload({ keyword: '', group: '', role: '', status: '' }))
      .toEqual({
        keyword: undefined,
        group: undefined,
        role: undefined,
        status: undefined,
      })
  })
})

describe('reset card grant completeness', () => {
  test('users mode needs at least one recipient', () => {
    expect(isResetCardGrantDraftComplete(draft({ recipientMode: 'users' }))).toBe(
      false
    )
    expect(
      isResetCardGrantDraftComplete(
        draft({ recipientMode: 'users', userIds: [4] })
      )
    ).toBe(true)
  })

  test('filter and all modes resolve recipients server-side', () => {
    expect(
      isResetCardGrantDraftComplete(draft({ recipientMode: 'filter', userIds: [] }))
    ).toBe(true)
    expect(
      isResetCardGrantDraftComplete(draft({ recipientMode: 'all', userIds: [] }))
    ).toBe(true)
  })

  test('codes mode needs no recipients', () => {
    expect(
      isResetCardGrantDraftComplete(draft({ recipientMode: 'codes', userIds: [] }))
    ).toBe(true)
  })

  test('a subscription target is refused outside users mode', () => {
    const target = {
      targetScope: 'subscription' as const,
      userSubscriptionId: '7',
    }
    expect(
      isResetCardGrantDraftComplete(
        draft({ ...target, recipientMode: 'users', userIds: [4] })
      )
    ).toBe(true)
    expect(
      isResetCardGrantDraftComplete(
        draft({ ...target, recipientMode: 'codes', userIds: [] })
      )
    ).toBe(false)
  })

  test('an empty reset scope is never complete', () => {
    expect(
      isResetCardGrantDraftComplete(
        draft({
          recipientMode: 'users',
          userIds: [4],
          scope: {
            advance_reset_time: true,
            reset_amount_used: false,
            reset_cycle_limits: false,
            reset_window_limits: false,
          },
        })
      )
    ).toBe(false)
  })
})

describe('reset card key export', () => {
  test('flattens every item key in order and skips empties', () => {
    expect(
      resetCardGrantKeys([
        { keys: ['a', 'b'] },
        { keys: [] },
        {},
        { keys: ['c', ''] },
      ])
    ).toEqual(['a', 'b', 'c'])
  })

  test('an absent result exports nothing', () => {
    expect(resetCardGrantKeys(undefined)).toEqual([])
  })

  test('an empty key list does not touch the DOM', () => {
    // The download helper short-circuits, so nothing is created or appended.
    const created: string[] = []
    const originalCreate = document.createElement.bind(document)
    document.createElement = ((tag: string) => {
      created.push(tag)
      return originalCreate(tag)
    }) as typeof document.createElement
    try {
      downloadResetCardKeys([], 'reset-card-keys')
      expect(created).not.toContain('a')
    } finally {
      document.createElement = originalCreate
    }
  })
})
