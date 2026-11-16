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
import { getRouteApi } from '@tanstack/react-router'
import type { OnChangeFn, SortingState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { getAdminPlans, listAllSubscriptions } from '../api'
import type {
  SubscriptionListItem,
  SubscriptionSortBy,
  SubscriptionSortOrder,
} from '../types'
import { SubscriptionsListBulkActions } from './subscriptions-list-bulk-actions'
import { useSubscriptionsListColumns } from './subscriptions-list-columns'
import { useSubscriptions } from './subscriptions-provider'

const route = getRouteApi('/_authenticated/subscriptions/$section')

/**
 * Column ids that the server can order by. The mapping is not identity-based:
 * `user` sorts by user_id and `usage` by amount_used, because those columns
 * render values derived from those fields.
 */
const SUBSCRIPTION_SORT_COLUMNS: Record<string, SubscriptionSortBy> = {
  id: 'id',
  user: 'user_id',
  plan: 'plan_id',
  status: 'status',
  usage: 'amount_used',
  end_time: 'end_time',
  next_reset_time: 'next_reset_time',
}

/**
 * Cross-user subscription list. This is what makes bulk management possible:
 * every subscription across every user on one page, selectable, with grant,
 * reset, cancel and delete applied to the whole selection at once.
 */
export function SubscriptionsListTable() {
  const { t } = useTranslation()
  const columns = useSubscriptionsListColumns()
  const { refreshTrigger } = useSubscriptions()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const [sorting, setSorting] = useState<SortingState>([])

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: {
      pageKey: 'sPage',
      pageSizeKey: 'sPageSize',
      defaultPage: 1,
      defaultPageSize: isMobile ? 10 : 20,
    },
    globalFilter: { enabled: true, key: 'sFilter' },
    columnFilters: [
      { columnId: 'status', searchKey: 'sStatus', type: 'array' },
      { columnId: 'plan', searchKey: 'sPlan', type: 'string' },
    ],
  })

  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const statusFilterValue = statusFilter[0] ?? ''
  const planFilterValue =
    (columnFilters.find((filter) => filter.id === 'plan')?.value as
      | string
      | undefined) ?? ''

  const sortParams = useMemo(() => {
    const activeSort = sorting[0]
    if (!activeSort) return {}
    const sortBy = SUBSCRIPTION_SORT_COLUMNS[activeSort.id]
    if (!sortBy) return {}
    return {
      sort_by: sortBy,
      sort_order: (activeSort.desc ? 'desc' : 'asc') as SubscriptionSortOrder,
    }
  }, [sorting])

  const handleSortingChange: OnChangeFn<SortingState> = (updater) => {
    setSorting(updater)
    if (pagination.pageIndex > 0) {
      onPaginationChange({ ...pagination, pageIndex: 0 })
    }
  }

  // The plan filter is a Combobox of plan titles, but the table filter state
  // stores the plan id so the query needs no title lookup.
  const { data: plansData } = useQuery({
    queryKey: ['admin-subscription-plans'],
    queryFn: getAdminPlans,
    staleTime: 60_000,
  })
  const planFilterOptions = useMemo(
    () =>
      (plansData?.data ?? []).map((record) => ({
        label: record.plan.title || `#${record.plan.id}`,
        value: String(record.plan.id),
      })),
    [plansData]
  )

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'admin-subscriptions',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      statusFilterValue,
      planFilterValue,
      sortParams,
      refreshTrigger,
    ],
    queryFn: async () => {
      const result = await listAllSubscriptions({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        keyword: globalFilter?.trim() || undefined,
        status: statusFilterValue || undefined,
        plan_id: planFilterValue ? Number(planFilterValue) : undefined,
        ...sortParams,
      })
      if (!result.success) {
        throw createServerError(result, t('Failed to load subscriptions'))
      }
      return {
        items: result.data?.items || [],
        total: result.data?.total || 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const subscriptions = useMemo(() => data?.items || [], [data])

  const { table } = useDataTable({
    data: subscriptions as SubscriptionListItem[],
    columns,
    enableRowSelection: true,
    getRowId: (row) => String(row.subscription.id),
    columnFilters,
    globalFilter,
    pagination,
    sorting,
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    onSortingChange: handleSortingChange,
    manualPagination: true,
    manualFiltering: true,
    manualSorting: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No subscriptions found')}
      emptyDescription={t(
        'No user subscription matches the current filters. Grant a plan to a user to create one.'
      )}
      skeletonKeyPrefix='subscriptions-list-skeleton'
      applyHeaderSize
      toolbarProps={{
        searchPlaceholder: t('Filter by username or user ID...'),
        searchDebounceMs: 500,
        filters: [
          {
            columnId: 'status',
            title: t('Status'),
            singleSelect: true,
            options: [
              { label: t('Active'), value: 'active' },
              { label: t('Expired'), value: 'expired' },
              { label: t('Invalidated'), value: 'cancelled' },
            ],
          },
          {
            columnId: 'plan',
            title: t('Plan'),
            singleSelect: true,
            options: planFilterOptions,
          },
        ],
      }}
      showMobileBulkActions
      mobileProps={{ enableRowSelection: true }}
      bulkActions={<SubscriptionsListBulkActions table={table} />}
    />
  )
}
