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
import { useMutation } from '@tanstack/react-query'
import type { Table } from '@tanstack/react-table'
import { Ban, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import {
  batchDeleteSubscriptionResetCards,
  batchInvalidateSubscriptionResetCards,
} from '../api'
import type {
  SubscriptionBatchResult,
  SubscriptionResetCardListItem,
} from '../types'
import { useSubscriptions } from './subscriptions-provider'

type DataTableBulkActionsProps = {
  table: Table<SubscriptionResetCardListItem>
}

type PendingAction = 'invalidate' | 'delete' | null

/**
 * Bulk status changes for reset cards. Invalidation only reaches unused cards:
 * a spent card keeps its redemption record, and the server reports that as a
 * per-card failure rather than silently ignoring it.
 */
export function ResetCardsBulkActions(props: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { triggerRefresh } = useSubscriptions()
  const [pendingAction, setPendingAction] = useState<PendingAction>(null)

  const selectedRows = props.table.getFilteredSelectedRowModel().rows
  const selectedIds = selectedRows.map((row) => row.original.card.id)

  const invalidate = useMutation({
    mutationFn: async (ids: number[]) => {
      const result = await batchInvalidateSubscriptionResetCards(ids)
      if (!result.success) throw createServerError(result)
      return result.data as SubscriptionBatchResult
    },
    onSuccess: (result) => {
      if (result.failed > 0) {
        toast.error(
          t('{{succeeded}} invalidated, {{failed}} failed', {
            succeeded: result.succeeded,
            failed: result.failed,
          })
        )
      } else {
        toast.success(
          t('Invalidated {{count}} reset cards', { count: result.succeeded })
        )
      }
      props.table.resetRowSelection()
      setPendingAction(null)
      triggerRefresh()
    },
    onError: (error) => {
      handleServerError(error, t('Failed to invalidate reset cards'))
    },
  })

  const remove = useMutation({
    mutationFn: async (ids: number[]) => {
      const result = await batchDeleteSubscriptionResetCards(ids)
      if (!result.success) throw createServerError(result)
      return result.data as SubscriptionBatchResult
    },
    onSuccess: (result) => {
      if (result.failed > 0) {
        toast.error(
          t('{{succeeded}} deleted, {{failed}} failed', {
            succeeded: result.succeeded,
            failed: result.failed,
          })
        )
      } else {
        toast.success(
          t('Deleted {{count}} reset cards', { count: result.succeeded })
        )
      }
      props.table.resetRowSelection()
      setPendingAction(null)
      triggerRefresh()
    },
    onError: (error) => {
      handleServerError(error, t('Failed to delete reset cards'))
    },
  })

  const isBusy = invalidate.isPending || remove.isPending

  return (
    <>
      <BulkActionsToolbar table={props.table} entityName={t('reset card')}>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                className='text-destructive hover:text-destructive size-8'
                aria-label={t('Disable selected reset cards')}
                disabled={isBusy}
                onClick={() => setPendingAction('invalidate')}
              />
            }
          >
            <Ban aria-hidden='true' />
          </TooltipTrigger>
          <TooltipContent>{t('Disable selected reset cards')}</TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='destructive'
                size='icon'
                className='size-8'
                aria-label={t('Delete selected reset cards')}
                disabled={isBusy}
                onClick={() => setPendingAction('delete')}
              />
            }
          >
            <Trash2 aria-hidden='true' />
          </TooltipTrigger>
          <TooltipContent>{t('Delete selected reset cards')}</TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      <ConfirmDialog
        open={pendingAction === 'invalidate'}
        onOpenChange={(open) => {
          if (!open && !isBusy) setPendingAction(null)
        }}
        title={t('Disable {{count}} reset cards?', {
          count: selectedIds.length,
        })}
        desc={t(
          'Disabled cards can no longer be redeemed. Cards that were already used keep their redemption record and are reported as failed.'
        )}
        confirmText={invalidate.isPending ? t('Disabling...') : t('Disable')}
        isLoading={invalidate.isPending}
        disabled={selectedIds.length === 0}
        handleConfirm={() => {
          if (selectedIds.length > 0 && !isBusy) invalidate.mutate(selectedIds)
        }}
      />

      <ConfirmDialog
        destructive
        open={pendingAction === 'delete'}
        onOpenChange={(open) => {
          if (!open && !isBusy) setPendingAction(null)
        }}
        title={t('Delete {{count}} reset cards?', {
          count: selectedIds.length,
        })}
        desc={t(
          'This permanently removes the selected cards, including their redemption records. Continue?'
        )}
        confirmText={remove.isPending ? t('Deleting...') : t('Delete')}
        isLoading={remove.isPending}
        disabled={selectedIds.length === 0}
        handleConfirm={() => {
          if (selectedIds.length > 0 && !isBusy) remove.mutate(selectedIds)
        }}
      />
    </>
  )
}
