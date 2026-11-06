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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { listAllSubscriptions } from '../api'
import type { SubscriptionListItem } from '../types'
import { SubscriptionsListBulkActions } from './subscriptions-list-bulk-actions'
import { useSubscriptionsListColumns } from './subscriptions-list-columns'
import { useSubscriptions } from './subscriptions-provider'

const route = getRouteApi('/_authenticated/subscriptions/$section')

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
    ],
  })

  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const statusFilterValue = statusFilter[0] ?? ''

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'admin-subscriptions',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      statusFilterValue,
      refreshTrigger,
    ],
    queryFn: async () => {
      const result = await listAllSubscriptions({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        keyword: globalFilter?.trim() || undefined,
        status: statusFilterValue || undefined,
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
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
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
        ],
      }}
      showMobileBulkActions
      mobileProps={{ enableRowSelection: true }}
      bulkActions={<SubscriptionsListBulkActions table={table} />}
    />
  )
}
