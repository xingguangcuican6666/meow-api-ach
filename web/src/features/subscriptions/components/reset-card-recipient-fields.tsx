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
import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import { Combobox } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { getGroups, searchUsers } from '@/features/users/api'
import {
  getUserRoleOptions,
  getUserStatusOptions,
  USER_STATUS,
} from '@/features/users/constants'
import { useDebounce } from '@/hooks/use-debounce'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  RESET_CARD_RECIPIENT_MODE_LABELS,
  type ResetCardGrantDraft,
} from '../lib/reset-card-grant'
import {
  RESET_CARD_RECIPIENT_MODES,
  type ResetCardRecipientMode,
} from '../types'

/**
 * How many recipients one batch may address. The server enforces the same
 * bound; checking here turns a round trip into an inline hint.
 */
export const RESET_CARD_RECIPIENT_LIMIT = 500

interface Props {
  draft: ResetCardGrantDraft
  onChange: (draft: ResetCardGrantDraft) => void
  disabled?: boolean
}

/**
 * Live recipient count for the server-resolved modes.
 *
 * Both the form (to show the number) and the dialog (to size the batch and
 * block submission) need it. Sharing the query key means TanStack Query serves
 * both from one request instead of polling the user table twice.
 *
 * `status` defaults to enabled for the same reason the server does: an empty
 * status also matches soft-deleted users, which would overstate the audience.
 */
export function useResetCardRecipientPreview(draft: ResetCardGrantDraft) {
  const isFilter = draft.recipientMode === 'filter'
  const enabled =
    draft.recipientMode === 'filter' || draft.recipientMode === 'all'

  // Debounce each condition separately: useDebounce compares by reference, so
  // handing it a freshly built object would restart the timer on every render.
  const keyword = useDebounce(isFilter ? draft.filters.keyword.trim() : '', 500)
  const group = useDebounce(isFilter ? draft.filters.group : '', 500)
  const role = useDebounce(isFilter ? draft.filters.role : '', 500)
  const status = useDebounce(isFilter ? draft.filters.status : '', 500)

  const query = useQuery({
    queryKey: [
      'reset-card-recipient-preview',
      draft.recipientMode,
      keyword,
      group,
      role,
      status,
    ],
    queryFn: async () => {
      const result = await searchUsers({
        keyword,
        group,
        role,
        status: status || String(USER_STATUS.ENABLED),
        p: 1,
        page_size: 1,
      })
      return result.data?.total ?? 0
    },
    enabled,
    staleTime: 5_000,
  })

  const total = query.data
  return {
    total,
    isFetching: query.isFetching,
    overLimit:
      enabled &&
      typeof total === 'number' &&
      total > RESET_CARD_RECIPIENT_LIMIT,
  }
}

/**
 * Who receives the cards. Four modes cover the real cases: a handful of named
 * users, everyone matching the conditions an admin would filter the users table
 * by, literally everyone, or nobody in particular (ownerless codes an operator
 * hands out in an activity).
 *
 * Only the explicit mode carries a user list. The other modes send conditions
 * and let the server resolve them, so a filter grant never silently drops
 * recipients to the page the browser happened to load.
 */
export function ResetCardRecipientFields(props: Props) {
  const { t } = useTranslation()
  const draft = props.draft
  const set = (patch: Partial<ResetCardGrantDraft>) =>
    props.onChange({ ...draft, ...patch })
  const setFilter = (patch: Partial<ResetCardGrantDraft['filters']>) =>
    set({ filters: { ...draft.filters, ...patch } })
  const [userKeyword, setUserKeyword] = useState('')
  const idPrefix = useId()

  // Remote user search: the picker only needs a handful of candidates, and a
  // local list of every user would not scale.
  const { data: userSearch } = useQuery({
    queryKey: ['reset-card-user-search', userKeyword],
    queryFn: () => searchUsers({ keyword: userKeyword, p: 1, page_size: 20 }),
    enabled: draft.recipientMode === 'users' && userKeyword.trim().length > 0,
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

  const { data: groupsData } = useQuery({
    queryKey: ['groups'],
    queryFn: async () => requireServerSuccess(await getGroups()),
    staleTime: 5 * 60 * 1000,
    enabled: draft.recipientMode === 'filter',
  })
  const groupOptions = useMemo(
    () => (groupsData?.data ?? []).map((group) => ({ label: group, value: group })),
    [groupsData]
  )

  // The role options carry a component in `icon`, which the legacy Combobox
  // overload does not accept; the filter only needs the label.
  const roleOptions = useMemo(
    () =>
      getUserRoleOptions(t).map((option) => ({
        label: option.label,
        value: option.value,
      })),
    [t]
  )
  // "Deleted" is not a recipient set, so it is not offered here.
  const statusOptions = useMemo(
    () =>
      getUserStatusOptions(t).filter(
        (option) => option.value !== String(USER_STATUS.DELETED)
      ),
    [t]
  )

  const preview = useResetCardRecipientPreview(draft)
  const needsPreview =
    draft.recipientMode === 'filter' || draft.recipientMode === 'all'

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex flex-col gap-2'>
        <span className='text-sm font-medium'>{t('Recipients')}</span>
        <RadioGroup
          value={draft.recipientMode}
          onValueChange={(value) => {
            if (typeof value !== 'string') return
            if (
              !RESET_CARD_RECIPIENT_MODES.includes(
                value as ResetCardRecipientMode
              )
            ) {
              return
            }
            const nextMode = value as ResetCardRecipientMode
            // A subscription belongs to one person, so an ownerless code or a
            // bulk filter could never match it. Fall back to the wide scope
            // instead of leaving a value the server would reject.
            if (nextMode !== 'users' && draft.targetScope === 'subscription') {
              set({ recipientMode: nextMode, targetScope: 'all' })
              return
            }
            set({ recipientMode: nextMode })
          }}
          disabled={props.disabled}
          aria-label={t('Recipients')}
          className='grid gap-2 sm:grid-cols-2'
        >
          {RESET_CARD_RECIPIENT_MODES.map((mode) => (
            <Label
              key={mode}
              htmlFor={`${idPrefix}-${mode}`}
              className='hover:bg-muted/50 has-data-checked:border-primary has-data-checked:bg-primary/5 flex cursor-pointer items-center gap-2 rounded-lg border p-2.5 font-normal transition-colors'
            >
              <RadioGroupItem id={`${idPrefix}-${mode}`} value={mode} />
              <span className='text-sm'>
                {t(RESET_CARD_RECIPIENT_MODE_LABELS[mode])}
              </span>
            </Label>
          ))}
        </RadioGroup>
      </div>

      {draft.recipientMode === 'users' ? (
        <div className='flex flex-col gap-2'>
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
            filterByLabel
          />
          <p className='text-muted-foreground text-xs'>
            {t('{{count}} recipient(s) selected', {
              count: draft.userIds.length,
            })}
          </p>
        </div>
      ) : null}

      {draft.recipientMode === 'filter' ? (
        <div className='flex flex-col gap-3'>
          <div className='flex flex-col gap-2'>
            <label
              htmlFor={`${idPrefix}-filter-keyword`}
              className='text-sm font-medium'
            >
              {t('Keyword')}
            </label>
            <Input
              id={`${idPrefix}-filter-keyword`}
              value={draft.filters.keyword}
              placeholder={t('Username, display name or user ID')}
              disabled={props.disabled}
              onChange={(event) => setFilter({ keyword: event.target.value })}
            />
          </div>
          <div className='grid gap-3 sm:grid-cols-3'>
            <div className='flex flex-col gap-2'>
              <span className='text-sm font-medium'>{t('Group')}</span>
              <Combobox
                options={groupOptions}
                value={draft.filters.group || null}
                onValueChange={(value: string | null) =>
                  setFilter({ group: value ?? '' })
                }
                placeholder={t('Any group')}
                disabled={props.disabled}
                className='w-full'
              />
            </div>
            <div className='flex flex-col gap-2'>
              <span className='text-sm font-medium'>{t('Role')}</span>
              <Combobox
                options={roleOptions}
                value={draft.filters.role || null}
                onValueChange={(value: string | null) =>
                  setFilter({ role: value ?? '' })
                }
                placeholder={t('Any role')}
                disabled={props.disabled}
                className='w-full'
              />
            </div>
            <div className='flex flex-col gap-2'>
              <span className='text-sm font-medium'>{t('Status')}</span>
              <Combobox
                options={statusOptions}
                value={draft.filters.status || null}
                onValueChange={(value: string | null) =>
                  setFilter({ status: value ?? '' })
                }
                placeholder={t('Enabled users')}
                disabled={props.disabled}
                className='w-full'
              />
            </div>
          </div>
        </div>
      ) : null}

      {draft.recipientMode === 'all' ? (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Cards go to every enabled user. Disabled and deleted accounts are skipped.'
          )}
        </p>
      ) : null}

      {draft.recipientMode === 'codes' ? (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Codes are not bound to anyone. Any signed-in user who holds one can redeem it.'
          )}
        </p>
      ) : null}

      {needsPreview ? (
        <p
          className={
            preview.overLimit
              ? 'text-destructive text-xs font-medium'
              : 'text-muted-foreground text-xs'
          }
        >
          {preview.isFetching
            ? t('Counting matching users...')
            : preview.overLimit
              ? t(
                  '{{count}} users match, over the {{limit}} per batch. Narrow the filter.',
                  {
                    count: preview.total ?? 0,
                    limit: RESET_CARD_RECIPIENT_LIMIT,
                  }
                )
              : t('{{count}} user(s) will receive cards', {
                  count: preview.total ?? 0,
                })}
        </p>
      ) : null}
    </div>
  )
}
