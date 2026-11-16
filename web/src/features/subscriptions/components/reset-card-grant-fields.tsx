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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DateTimePicker } from '@/components/datetime-picker'
import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { searchUsers } from '@/features/users/api'

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
}

/**
 * The grant form shared by the reset-card section's primary button. User ids are
 * entered as chips: each chip is one recipient, and the count field mints that
 * many independent cards per recipient.
 */
export function ResetCardGrantFields(props: Props) {
  const { t } = useTranslation()
  const draft = props.draft
  const set = (patch: Partial<ResetCardGrantDraft>) =>
    props.onChange({ ...draft, ...patch })
  const [userKeyword, setUserKeyword] = useState('')

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

  // Remote user search: the picker only needs a handful of candidates, and a
  // local list of every user would not scale.
  const { data: userSearch } = useQuery({
    queryKey: ['reset-card-user-search', userKeyword],
    queryFn: () => searchUsers({ keyword: userKeyword, p: 1, page_size: 20 }),
    enabled: userKeyword.trim().length > 0,
    staleTime: 10_000,
  })
  const userOptions = useMemo(
    () =>
      (userSearch?.data?.items ?? []).map((user) => ({
        label: user.username || `#${user.id}`,
        value: String(user.id),
        hint: user.display_name || undefined,
      })),
    [userSearch]
  )

  // Typed as plain strings so the legacy Combobox overload is selected: a
  // literal-union value would make its narrow change handler fail the
  // contravariant check against `(value: string | null) => void`.
  const scopeOptions: Array<{ value: string; label: string }> =
    SUBSCRIPTION_RESET_CARD_SCOPES.map((scope) => ({
      value: scope,
      label: t(RESET_CARD_SCOPE_LABELS[scope]),
    }))

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-col gap-2'>
        <label className='text-sm font-medium'>{t('Recipients')}</label>
        <MultiSelect
          options={userOptions}
          selected={draft.userIds.map(String)}
          onChange={(values) =>
            set({ userIds: values.map((value) => Number.parseInt(value, 10)) })
          }
          placeholder={t('Search users by username or ID...')}
          emptyText={t('Type a username or user ID to search')}
          disabled={props.disabled}
          onInputValueChange={setUserKeyword}
        />
        <p className='text-muted-foreground text-xs'>
          {t('{{count}} recipient(s) selected', {
            count: draft.userIds.length,
          })}
        </p>
      </div>

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
