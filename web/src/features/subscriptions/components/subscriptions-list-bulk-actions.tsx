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
import { Ban, RotateCcw, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import {
  batchDeleteSubscriptions,
  batchInvalidateSubscriptions,
  batchResetSubscriptions,
} from '../api'
import type { SubscriptionBatchResult, SubscriptionListItem } from '../types'
import { useSubscriptions } from './subscriptions-provider'

type DataTableBulkActionsProps = {
  table: Table<SubscriptionListItem>
}

type PendingAction = 'invalidate' | 'delete' | null

/**
 * Reports a batch outcome to the operator. A batch is a list of independent
 * operations, so a partial success is stated as such instead of being reported
 * as a plain success or a plain failure.
 */
function reportBatchOutcome(
  result: SubscriptionBatchResult,
  labels: {
    allDone: (count: number) => string
    partial: (succeeded: number, failed: number) => string
  }
) {
  if (result.failed > 0) {
    toast.error(labels.partial(result.succeeded, result.failed))
    return
  }
  toast.success(labels.allDone(result.succeeded))
}

export function SubscriptionsListBulkActions(props: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { triggerRefresh } = useSubscriptions()
  const [pendingAction, setPendingAction] = useState<PendingAction>(null)
  const [resetTargets, setResetTargets] = useState<number[] | null>(null)
  const [advanceResetTime, setAdvanceResetTime] = useState(true)
  useEffect(() => {
    if (resetTargets !== null) setAdvanceResetTime(true)
  }, [resetTargets])
  const selectedRows = props.table.getFilteredSelectedRowModel().rows

  const selectedIds = selectedRows.map((row) => row.original.subscription.id)

  const invalidate = useMutation({
    mutationFn: async (ids: number[]) => {
      const result = await batchInvalidateSubscriptions(ids)
      if (!result.success) throw createServerError(result)
      return result.data as SubscriptionBatchResult
    },
    onSuccess: (result) => {
      reportBatchOutcome(result, {
        allDone: (count) => t('Invalidated {{count}} subscriptions', { count }),
        partial: (succeeded, failed) =>
          t('{{succeeded}} invalidated, {{failed}} failed', {
            succeeded,
            failed,
          }),
      })
      props.table.resetRowSelection()
      setPendingAction(null)
      triggerRefresh()
    },
    onError: (error) => {
      handleServerError(error, t('Failed to invalidate subscriptions'))
    },
  })

  const remove = useMutation({
    mutationFn: async (ids: number[]) => {
      const result = await batchDeleteSubscriptions(ids)
      if (!result.success) throw createServerError(result)
      return result.data as SubscriptionBatchResult
    },
    onSuccess: (result) => {
      reportBatchOutcome(result, {
        allDone: (count) => t('Deleted {{count}} subscriptions', { count }),
        partial: (succeeded, failed) =>
          t('{{succeeded}} deleted, {{failed}} failed', { succeeded, failed }),
      })
      props.table.resetRowSelection()
      setPendingAction(null)
      triggerRefresh()
    },
    onError: (error) => {
      handleServerError(error, t('Failed to delete subscriptions'))
    },
  })

  const reset = useMutation({
    mutationFn: async (ids: number[]) => {
      const result = await batchResetSubscriptions({
        ids,
        advance_reset_time: advanceResetTime,
      })
      if (!result.success) throw createServerError(result)
      return result.data as SubscriptionBatchResult
    },
    onSuccess: (result) => {
      reportBatchOutcome(result, {
        allDone: (count) => t('Reset {{count}} subscriptions', { count }),
        partial: (succeeded, failed) =>
          t('{{succeeded}} reset, {{failed}} failed', { succeeded, failed }),
      })
      setResetTargets(null)
      props.table.resetRowSelection()
      triggerRefresh()
    },
    onError: (error) => {
      handleServerError(error, t('Failed to reset subscriptions'))
    },
  })

  const pendingIds =
    pendingAction === null
      ? []
      : selectedRows.map((row) => row.original.subscription.id)
  // A reset in flight counts as busy too: it rewrites the same rows the other
  // two actions delete or cancel, so letting them start mid-reset would let the
  // operator act on counters that are about to change.
  const isBusy = invalidate.isPending || remove.isPending || reset.isPending

  return (
    <>
      <BulkActionsToolbar table={props.table} entityName={t('subscription')}>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                className='size-8'
                aria-label={t('Reset selected subscription quota')}
                disabled={isBusy}
                onClick={() => setResetTargets(selectedIds)}
              />
            }
          >
            <RotateCcw aria-hidden='true' />
          </TooltipTrigger>
          <TooltipContent>
            {t('Reset selected subscription quota')}
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                className='text-destructive hover:text-destructive size-8'
                aria-label={t('Invalidate selected subscriptions')}
                disabled={isBusy}
                onClick={() => setPendingAction('invalidate')}
              />
            }
          >
            <Ban aria-hidden='true' />
          </TooltipTrigger>
          <TooltipContent>
            {t('Invalidate selected subscriptions')}
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='destructive'
                size='icon'
                className='size-8'
                aria-label={t('Delete selected subscriptions')}
                disabled={isBusy}
                onClick={() => setPendingAction('delete')}
              />
            }
          >
            <Trash2 aria-hidden='true' />
          </TooltipTrigger>
          <TooltipContent>{t('Delete selected subscriptions')}</TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      <ConfirmDialog
        open={pendingAction === 'invalidate'}
        onOpenChange={(open) => {
          if (!open && !isBusy) setPendingAction(null)
        }}
        title={t('Invalidate {{count}} subscriptions?', {
          count: pendingIds.length,
        })}
        desc={t(
          'The selected subscriptions stop working immediately and their users fall back to the plan downgrade group.'
        )}
        confirmText={
          invalidate.isPending ? t('Invalidating...') : t('Invalidate')
        }
        isLoading={invalidate.isPending}
        disabled={pendingIds.length === 0}
        handleConfirm={() => {
          if (pendingIds.length > 0 && !isBusy) invalidate.mutate(pendingIds)
        }}
      />

      <ConfirmDialog
        destructive
        open={pendingAction === 'delete'}
        onOpenChange={(open) => {
          if (!open && !isBusy) setPendingAction(null)
        }}
        title={t('Delete {{count}} subscriptions?', {
          count: pendingIds.length,
        })}
        desc={t(
          'This permanently removes the selected subscriptions. Usage already charged is not refunded.'
        )}
        confirmText={remove.isPending ? t('Deleting...') : t('Delete')}
        isLoading={remove.isPending}
        disabled={pendingIds.length === 0}
        handleConfirm={() => {
          if (pendingIds.length > 0 && !isBusy) remove.mutate(pendingIds)
        }}
      />

      <ConfirmDialog
        open={resetTargets !== null}
        onOpenChange={(open) => {
          if (!open && !reset.isPending) setResetTargets(null)
        }}
        title={t('Reset {{count}} subscriptions?', {
          count: resetTargets?.length ?? 0,
        })}
        desc={t(
          'This zeroes the usage counters of the selected subscriptions so their quota is available again.'
        )}
        confirmText={reset.isPending ? t('Resetting...') : t('Reset')}
        isLoading={reset.isPending}
        disabled={!resetTargets?.length}
        handleConfirm={() => {
          if (resetTargets?.length && !reset.isPending) {
            reset.mutate(resetTargets)
          }
        }}
      >
        <label className='flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm'>
          <span>{t('Advance next reset time')}</span>
          {/* The wrapping <label> already names this switch; an extra

              aria-label would concatenate into a doubled accessible name. */}

          <Switch
            checked={advanceResetTime}
            onCheckedChange={(checked) => setAdvanceResetTime(!!checked)}
          />
        </label>
      </ConfirmDialog>
    </>
  )
}
