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
import type { ColumnDef } from '@tanstack/react-table'
import type { TFunction } from 'i18next'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { formatTimestamp } from '../lib'
import type {
  SubscriptionResetCardListItem,
  SubscriptionResetCardStatus,
} from '../types'

const STATUS_VARIANTS = {
  unused: 'success',
  used: 'neutral',
  disabled: 'warning',
} as const

const STATUS_LABELS: Record<SubscriptionResetCardStatus, string> = {
  unused: 'Unused',
  used: 'Used',
  disabled: 'Disabled',
}

/**
 * One line naming what a card resets. The card freezes its configuration at
 * grant time, so this is a read-only summary of that frozen state — it never
 * derives anything from the target subscriptions.
 */
function describeCard(
  item: SubscriptionResetCardListItem,
  t: TFunction
): string {
  const card = item.card
  const parts: string[] = []
  if (card.reset_amount_used) parts.push(t('total quota usage'))
  if (card.reset_cycle_limits) parts.push(t('cycle limit counters'))
  if (card.reset_window_limits) parts.push(t('rolling window counters'))
  return parts.length > 0 ? parts.join(', ') : t('Nothing selected')
}

function describeTarget(
  item: SubscriptionResetCardListItem,
  t: TFunction
): string {
  const card = item.card
  switch (card.target_scope) {
    case 'plan':
      return t('Plan #{{id}}', { id: card.plan_id ?? '-' })
    case 'subscription':
      return t('Subscription #{{id}}', { id: card.user_subscription_id ?? '-' })
    default:
      return t('All active subscriptions')
  }
}

export function useResetCardsColumns(): ColumnDef<SubscriptionResetCardListItem>[] {
  const { t } = useTranslation()

  return useMemo(
    (): ColumnDef<SubscriptionResetCardListItem>[] => [
      {
        id: 'select',
        header: ({ table }) => (
          <Checkbox
            checked={table.getIsAllPageRowsSelected()}
            indeterminate={table.getIsSomePageRowsSelected()}
            onCheckedChange={(value) =>
              table.toggleAllPageRowsSelected(!!value)
            }
            aria-label={t('Select all')}
            className='translate-y-[2px]'
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            checked={row.getIsSelected()}
            onCheckedChange={(value) => row.toggleSelected(!!value)}
            aria-label={t('Select row')}
            className='translate-y-[2px]'
          />
        ),
        enableSorting: false,
        enableHiding: false,
        size: 40,
      },
      {
        accessorFn: (row) => row.card.id,
        id: 'id',
        header: t('ID'),
        meta: { mobileHidden: true },
        cell: ({ row }) => <TableId value={row.original.card.id} />,
        size: 70,
      },
      {
        accessorFn: (row) => row.card.name,
        id: 'name',
        header: t('Card name'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <div className='max-w-full min-w-0'>
            <div className='truncate font-medium'>
              {row.original.card.name || '-'}
            </div>
            <div className='text-muted-foreground text-xs'>
              {describeTarget(row.original, t)}
            </div>
          </div>
        ),
        size: 200,
      },
      {
        accessorFn: (row) => row.username,
        id: 'user',
        header: t('User'),
        cell: ({ row }) => {
          // Ownerless codes have no holder until someone redeems one.
          if (row.original.card.user_id === 0) {
            return (
              <span className='text-muted-foreground text-xs'>
                {t('Unbound')}
              </span>
            )
          }
          return (
            <div className='max-w-full min-w-0'>
              <div className='truncate'>{row.original.username || '-'}</div>
              <div className='text-muted-foreground text-xs'>
                ID: {row.original.card.user_id}
              </div>
            </div>
          )
        },
        size: 150,
      },
      {
        id: 'scope',
        header: t('Reset scope'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground text-xs'>
            {describeCard(row.original, t)}
          </span>
        ),
        size: 200,
      },
      {
        accessorFn: (row) => row.card.status,
        id: 'status',
        header: t('Status'),
        meta: { mobileBadge: true },
        cell: ({ row }) => {
          const status = row.original.card.status
          return (
            <StatusBadge
              label={t(STATUS_LABELS[status] ?? 'Unused')}
              variant={STATUS_VARIANTS[status] ?? 'neutral'}
              copyable={false}
            />
          )
        },
        size: 100,
      },
      {
        id: 'key',
        header: t('Key'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          // The full key is shown on demand: an admin list is not the place to
          // spray every redeemable secret into a screenshot.
          <Tooltip>
            <TooltipTrigger
              render={
                <span className='text-muted-foreground cursor-help font-mono text-xs' />
              }
            >
              {row.original.card.key.slice(0, 8)}…
            </TooltipTrigger>
            <TooltipContent>
              <span className='font-mono'>{row.original.card.key}</span>
            </TooltipContent>
          </Tooltip>
        ),
        size: 110,
      },
      {
        accessorFn: (row) => row.card.expired_time,
        id: 'expired_time',
        header: t('Expires At'),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const expired = Number(row.original.card.expired_time || 0)
          return (
            <span className='text-muted-foreground'>
              {expired > 0 ? formatTimestamp(expired) : t('Never expires')}
            </span>
          )
        },
        size: 170,
      },
      {
        accessorFn: (row) => row.card.used_time,
        id: 'used_time',
        header: t('Used At'),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const used = Number(row.original.card.used_time || 0)
          return (
            <span className='text-muted-foreground'>
              {used > 0 ? formatTimestamp(used) : '-'}
            </span>
          )
        },
        size: 170,
      },
      {
        accessorFn: (row) => row.card.created_time,
        id: 'created_time',
        header: t('Created At'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground'>
            {formatTimestamp(Number(row.original.card.created_time || 0))}
          </span>
        ),
        size: 170,
      },
    ],
    [t]
  )
}
