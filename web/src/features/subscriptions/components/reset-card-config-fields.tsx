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
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { DateTimePicker } from '@/components/datetime-picker'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'

import { getAdminPlans } from '../api'
import {
  RESET_CARD_SCOPE_LABELS,
  type ResetCardGrantDraft,
} from '../lib/reset-card-grant'
import {
  SUBSCRIPTION_RESET_CARD_SCOPES,
  type SubscriptionResetCardScope,
} from '../types'
import { SubscriptionResetScopeFields } from './subscription-reset-scope-fields'

interface Props {
  draft: ResetCardGrantDraft
  onChange: (draft: ResetCardGrantDraft) => void
  disabled?: boolean
  /**
   * Target scopes the caller cannot use. The code mode passes `subscription`:
   * an ownerless code could never match a subscription that belongs to one
   * person.
   */
  blockedScopes?: SubscriptionResetCardScope[]
}

/**
 * What a card does when redeemed — how many, what they are called, when they
 * expire, and which counters they clear. Shared by the admin grant dialog and
 * the users-page bulk action, which differ only in how they pick recipients.
 */
export function ResetCardConfigFields(props: Props) {
  const { t } = useTranslation()
  const draft = props.draft
  const set = (patch: Partial<ResetCardGrantDraft>) =>
    props.onChange({ ...draft, ...patch })

  const { data: plansData } = useQuery({
    queryKey: ['admin-subscription-plans', 'reset-card-options'],
    queryFn: getAdminPlans,
    staleTime: 60_000,
  })
  const planOptions = useMemo(
    () =>
      (plansData?.data ?? []).map((record) => ({
        label: record.plan.title || `#${record.plan.id}`,
        value: String(record.plan.id),
      })),
    [plansData]
  )

  // Typed as plain strings so the legacy Combobox overload is selected: a
  // literal-union value would make its narrow change handler fail the
  // contravariant check against `(value: string | null) => void`.
  const scopeOptions: Array<{ value: string; label: string }> =
    SUBSCRIPTION_RESET_CARD_SCOPES.filter(
      (scope) => !props.blockedScopes?.includes(scope)
    ).map((scope) => ({
      value: scope,
      label: t(RESET_CARD_SCOPE_LABELS[scope]),
    }))

  return (
    <div className='flex flex-col gap-4'>
      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='flex flex-col gap-2'>
          <label htmlFor='reset-card-count' className='text-sm font-medium'>
            {t('Cards per recipient')}
          </label>
          <Input
            id='reset-card-count'
            type='number'
            min={1}
            max={100}
            value={draft.count}
            disabled={props.disabled}
            onChange={(event) =>
              set({
                count: Math.max(
                  1,
                  Number.parseInt(event.target.value, 10) || 1
                ),
              })
            }
          />
        </div>
        <div className='flex flex-col gap-2'>
          <label htmlFor='reset-card-name' className='text-sm font-medium'>
            {t('Card name')}
          </label>
          <Input
            id='reset-card-name'
            value={draft.name}
            placeholder={t('Optional; defaults from the target scope')}
            disabled={props.disabled}
            onChange={(event) => set({ name: event.target.value })}
          />
        </div>
      </div>

      <div className='flex flex-col gap-2'>
        <label className='text-sm font-medium'>{t('Reset target')}</label>
        <Combobox
          options={scopeOptions}
          value={draft.targetScope}
          onValueChange={(value: string | null) =>
            set({ targetScope: (value ?? 'all') as SubscriptionResetCardScope })
          }
          placeholder={t('Select reset target')}
          disabled={props.disabled}
          className='w-full'
        />
      </div>

      <div className='flex flex-col gap-2'>
        <span className='text-sm font-medium'>{t('Expiration Time')}</span>
        <div className='flex items-center gap-2'>
          <DateTimePicker
            value={
              draft.expiredTime ? new Date(draft.expiredTime * 1000) : undefined
            }
            onChange={(date) =>
              set({ expiredTime: date ? Math.floor(date.getTime() / 1000) : 0 })
            }
            placeholder={t('Never expires')}
            className='flex-1'
          />
          {draft.expiredTime ? (
            <Button
              type='button'
              variant='ghost'
              size='sm'
              disabled={props.disabled}
              onClick={() => set({ expiredTime: 0 })}
            >
              {t('Clear')}
            </Button>
          ) : null}
        </div>
      </div>

      {draft.targetScope === 'plan' ? (
        <div className='flex flex-col gap-2'>
          <label className='text-sm font-medium'>
            {t('Subscription plan')}
          </label>
          <Combobox
            options={planOptions}
            value={draft.planId}
            onValueChange={(value: string | null) => set({ planId: value })}
            placeholder={t('Select subscription plan')}
            disabled={props.disabled || planOptions.length === 0}
            className='w-full'
          />
        </div>
      ) : null}

      {draft.targetScope === 'subscription' ? (
        <div className='flex flex-col gap-2'>
          <label
            htmlFor='reset-card-subscription-id'
            className='text-sm font-medium'
          >
            {t('Subscription ID')}
          </label>
          <Input
            id='reset-card-subscription-id'
            inputMode='numeric'
            value={draft.userSubscriptionId}
            placeholder={t('The recipient must own this subscription')}
            disabled={props.disabled}
            onChange={(event) =>
              set({ userSubscriptionId: event.target.value.trim() })
            }
          />
        </div>
      ) : null}

      <div className='flex flex-col gap-2'>
        <span className='text-sm font-medium'>{t('Reset scope')}</span>
        <SubscriptionResetScopeFields
          value={draft.scope}
          onChange={(scope) => set({ scope })}
          disabled={props.disabled}
        />
      </div>
    </div>
  )
}
