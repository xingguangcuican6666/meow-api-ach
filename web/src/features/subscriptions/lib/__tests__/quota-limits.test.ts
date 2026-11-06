/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { TFunction } from 'i18next'
import { describe, expect, test } from 'vitest'

import type { SubscriptionPlan } from '../../types'
import { formatQuotaLimitLabel, formatQuotaLimitWindow } from '../format'
import {
  PLAN_FORM_DEFAULTS,
  formValuesToPlanPayload,
  planToFormValues,
} from '../plan-form'

const t = ((key: string) => key) as TFunction

function basePlan(overrides: Partial<SubscriptionPlan> = {}): SubscriptionPlan {
  return {
    id: 1,
    title: 'Plan',
    price_amount: 10,
    currency: 'USD',
    duration_unit: 'month',
    duration_value: 1,
    quota_reset_period: 'never',
    enabled: true,
    sort_order: 0,
    max_purchase_per_user: 0,
    total_amount: 0,
    ...overrides,
  } as SubscriptionPlan
}

describe('plan quota limits form mapping', () => {
  test('a plan without limits loads as an empty list', () => {
    const values = planToFormValues(basePlan())
    expect(values.quota_limits).toEqual([])
  })

  test('defaults contain an empty limit list so the form starts without rows', () => {
    expect(PLAN_FORM_DEFAULTS.quota_limits).toEqual([])
  })

  test('a 5h, a 7d and a per-model limit all survive the round trip', () => {
    const plan = basePlan({
      quota_limits: [
        {
          key: 'h5',
          name: '5h',
          window_seconds: 18000,
          amount_total: 500000,
          models: [],
        },
        {
          key: 'd7',
          name: '7d',
          window_seconds: 604800,
          amount_total: 5000000,
          models: [],
        },
        {
          key: 'claude',
          name: 'Claude',
          window_seconds: 0,
          amount_total: 250000,
          models: ['claude-*'],
        },
      ],
    })

    const payload = formValuesToPlanPayload(planToFormValues(plan))

    expect(payload.plan.quota_limits).toEqual([
      {
        key: 'h5',
        name: '5h',
        window_seconds: 18000,
        amount_total: 500000,
        models: [],
      },
      {
        key: 'd7',
        name: '7d',
        window_seconds: 604800,
        amount_total: 5000000,
        models: [],
      },
      {
        key: 'claude',
        name: 'Claude',
        window_seconds: 0,
        amount_total: 250000,
        models: ['claude-*'],
      },
    ])
  })

  test('a limit without a key gets a stable auto key instead of being dropped', () => {
    const values = planToFormValues(basePlan())
    values.quota_limits = [
      {
        key: '',
        name: '',
        window_seconds: 18000,
        amount_total: 0,
        models: [' gpt-4o '],
      },
    ]

    const payload = formValuesToPlanPayload(values)

    expect(payload.plan.quota_limits).toHaveLength(1)
    expect(payload.plan.quota_limits?.[0].key).toBe('limit-1')
    expect(payload.plan.quota_limits?.[0].models).toEqual(['gpt-4o'])
  })
})

describe('quota limit display formatting', () => {
  test.each([
    { seconds: 0, want: 'Whole subscription period' },
    { seconds: 18000, want: '5 hours' },
    { seconds: 604800, want: '7 days' },
    { seconds: 3600, want: '1 hour' },
    { seconds: 86400, want: '1 day' },
    { seconds: 120, want: '2 minutes' },
    { seconds: 45, want: '45 seconds' },
  ])('renders $seconds seconds as $want', ({ seconds, want }) => {
    expect(formatQuotaLimitWindow(seconds, t)).toBe(want)
  })

  test('a per-model limit label names the window and the model', () => {
    expect(
      formatQuotaLimitLabel(
        { name: 'Claude', window_seconds: 0, model: 'claude-sonnet-4' },
        t
      )
    ).toBe('Claude · Whole subscription period · claude-sonnet-4')
  })

  test('a windowed limit label names the window when no model is set', () => {
    expect(
      formatQuotaLimitLabel({ name: '5h', window_seconds: 18000 }, t)
    ).toBe('5h · 5 hours')
  })

  test('an unnamed limit falls back to the window label', () => {
    expect(formatQuotaLimitLabel({ window_seconds: 604800 }, t)).toBe('7 days')
  })
})
