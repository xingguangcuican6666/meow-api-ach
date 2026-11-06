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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Checkbox } from '@/components/ui/checkbox'
import { Progress } from '@/components/ui/progress'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatQuota } from '@/lib/format'

import { formatQuotaLimitLabel, formatTimestamp } from '../lib'
import type { SubscriptionListItem } from '../types'
import { AllowedModelsLabel } from './allowed-models-label'

/**
 * A subscription's badge state. `status` is authoritative while the row is
 * inside its window; an end_time that has already passed wins over a stale
 * 'active' value, which is what the per-user management dialog reports too.
 */
function subscriptionState(row: SubscriptionListItem) {
  const sub = row.subscription
  const now = Date.now() / 1000
  const endTime = sub.end_time || 0
  if (endTime > 0 && endTime < now) {
    return { label: 'Expired', variant: 'warning' } as const
  }
  if (sub.status === 'cancelled') {
    return { label: 'Invalidated', variant: 'neutral' } as const
  }
  if (sub.status === 'active') {
    return { label: 'Active', variant: 'success' } as const
  }
  return { label: sub.status || '-', variant: 'neutral' } as const
}

export function useSubscriptionsListColumns(): ColumnDef<SubscriptionListItem>[] {
  const { t } = useTranslation()

  return useMemo(
    (): ColumnDef<SubscriptionListItem>[] => [
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
        accessorFn: (row) => row.subscription.id,
        id: 'id',
        header: t('ID'),
        meta: { mobileHidden: true },
        cell: ({ row }) => <TableId value={row.original.subscription.id} />,
        size: 70,
      },
      {
        accessorFn: (row) => row.username,
        id: 'user',
        header: t('User'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <div className='max-w-full min-w-0'>
            <div className='truncate font-medium'>
              {row.original.username || '-'}
            </div>
            <div className='text-muted-foreground text-xs'>
              ID: {row.original.user_id}
            </div>
          </div>
        ),
        size: 160,
      },
      {
        accessorFn: (row) => row.plan_title,
        id: 'plan',
        header: t('Plan'),
        cell: ({ row }) => (
          <div className='max-w-full min-w-0'>
            <div className='truncate'>{row.original.plan_title || '-'}</div>
            <div className='text-muted-foreground text-xs'>
              {t('Plan ID')}: {row.original.plan_id}
            </div>
            {/* A scoped plan is the difference between "this user has quota"
                and "this user has quota for these models", so name them. Only
                scoped plans need the line: showing "All models" beside every
                unscoped row would be noise. */}
            <AllowedModelsLabel
              allowed={row.original.allowed_models}
              hideWhenUnscoped
              className='text-[11px]'
            />
          </div>
        ),
        size: 180,
      },
      {
        id: 'status',
        header: t('Status'),
        cell: ({ row }) => {
          const state = subscriptionState(row.original)
          return (
            <StatusBadge
              label={t(state.label)}
              variant={state.variant}
              copyable={false}
            />
          )
        },
        size: 110,
      },
      {
        id: 'usage',
        header: t('Quota Usage'),
        cell: ({ row }) => {
          const sub = row.original.subscription
          const total = Number(sub.amount_total || 0)
          const used = Number(sub.amount_used || 0)
          const percent =
            total > 0 ? Math.min(100, Math.round((used / total) * 100)) : 0
          const limits = row.original.quota_limits || []
          return (
            <div className='flex min-w-[140px] flex-col gap-1'>
              <span className='text-muted-foreground text-xs'>
                {total > 0
                  ? `${formatQuota(used)}/${formatQuota(total)}`
                  : `${formatQuota(used)} · ${t('Unlimited')}`}
              </span>
              {total > 0 && <Progress value={percent} className='h-1' />}
              {limits.length > 0 && (
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <span className='text-muted-foreground/80 cursor-default truncate text-[11px]' />
                    }
                  >
                    {limits
                      .map(
                        (limit) =>
                          `${formatQuotaLimitLabel(limit, t)} ${formatQuota(Number(limit.amount_used))}/${formatQuota(Number(limit.amount_total))}`
                      )
                      .join(' · ')}
                  </TooltipTrigger>
                  <TooltipContent>
                    <div className='flex flex-col gap-0.5'>
                      {limits.map((limit) => (
                        <div
                          key={`${limit.key}:${limit.model || 'all'}`}
                          className='flex items-center justify-between gap-3'
                        >
                          <span>{formatQuotaLimitLabel(limit, t)}</span>
                          <span>
                            {formatQuota(Number(limit.amount_used))}/
                            {formatQuota(Number(limit.amount_total))}
                          </span>
                        </div>
                      ))}
                    </div>
                  </TooltipContent>
                </Tooltip>
              )}
            </div>
          )
        },
        size: 220,
      },
      {
        accessorFn: (row) => row.subscription.end_time,
        id: 'end_time',
        header: t('Expires At'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground'>
            {formatTimestamp(Number(row.original.subscription.end_time || 0))}
          </span>
        ),
        size: 170,
      },
      {
        accessorFn: (row) => row.subscription.next_reset_time,
        id: 'next_reset_time',
        header: t('Next Reset'),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const next = Number(row.original.subscription.next_reset_time || 0)
          return (
            <span className='text-muted-foreground'>
              {next > 0 ? formatTimestamp(next) : t('No Reset')}
            </span>
          )
        },
        size: 170,
      },
    ],
    [t]
  )
}
