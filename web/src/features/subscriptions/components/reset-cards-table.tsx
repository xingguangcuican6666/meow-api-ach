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
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { createServerError } from '@/lib/server-error-message'

import { listSubscriptionResetCards } from '../api'
import type { SubscriptionResetCardListItem } from '../types'
import { ResetCardsBulkActions } from './reset-cards-bulk-actions'
import { useResetCardsColumns } from './reset-cards-columns'
import { useSubscriptions } from './subscriptions-provider'

const route = getRouteApi('/_authenticated/subscriptions/$section')

/**
 * Root-only list of every reset card ever minted, across users. Cards are
 * immutable once granted except for status, so this view is mostly a ledger:
 * what was issued, to whom, whether it was spent, and which batch it belongs to.
 */
export function ResetCardsTable() {
  const { t } = useTranslation()
  const columns = useResetCardsColumns()
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
      { columnId: 'status', searchKey: 'sCardStatus', type: 'array' },
    ],
  })

  const statusFilter =
    (columnFilters.find((filter) => filter.id === 'status')?.value as
      | string[]
      | undefined) ?? []

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'admin-subscription-reset-cards',
      pagination.pageIndex + 1,
      pagination.pageSize,
      globalFilter,
      statusFilter,
      refreshTrigger,
    ],
    queryFn: async () => {
      const result = await listSubscriptionResetCards({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        keyword: globalFilter?.trim() || undefined,
        status: statusFilter[0] || undefined,
      })
      if (!result.success) {
        throw createServerError(result, t('Failed to load reset cards'))
      }
      return {
        items: result.data?.items || [],
        total: result.data?.total || 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const cards = data?.items || []

  const { table } = useDataTable({
    data: cards as SubscriptionResetCardListItem[],
    columns,
    enableRowSelection: true,
    getRowId: (row) => String(row.card.id),
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
      emptyTitle={t('No reset cards found')}
      emptyDescription={t(
        'Grant a reset card to a user and it appears here, with the configuration it will apply when redeemed.'
      )}
      skeletonKeyPrefix='reset-cards-skeleton'
      applyHeaderSize
      toolbarProps={{
        searchPlaceholder: t('Filter by card name, key or user ID...'),
        searchDebounceMs: 500,
        filters: [
          {
            columnId: 'status',
            title: t('Status'),
            singleSelect: true,
            options: [
              { label: t('Unused'), value: 'unused' },
              { label: t('Used'), value: 'used' },
              { label: t('Disabled'), value: 'disabled' },
            ],
          },
        ],
      }}
      showMobileBulkActions
      mobileProps={{ enableRowSelection: true }}
      bulkActions={<ResetCardsBulkActions table={table} />}
    />
  )
}
