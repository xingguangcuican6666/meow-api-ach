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
import { Download } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { RiskAcknowledgementDialog } from '@/components/risk-acknowledgement-dialog'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { grantSubscriptionResetCards } from '../../api'
import {
  EMPTY_RESET_CARD_GRANT_DRAFT,
  downloadResetCardKeys,
  isResetCardGrantDraftComplete,
  resetCardGrantKeys,
  resetCardGrantPayload,
  type ResetCardGrantDraft,
} from '../../lib/reset-card-grant'
import type { SubscriptionResetCardGrantResult } from '../../types'
import { ResetCardGrantFields } from '../reset-card-grant-fields'
import { useResetCardRecipientPreview } from '../reset-card-recipient-fields'
import { useSubscriptions } from '../subscriptions-provider'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Root's grant form. One submission mints `count` independent cards for every
 * resolved recipient, all sharing the same frozen configuration and batch id.
 *
 * Recipients are resolved in three different places: an explicit user list from
 * the picker, a filter/all condition evaluated by the server, or nowhere at all
 * for ownerless codes. The dialog only sizes the batch and gates submission; it
 * never expands a filter into a user list, because the server's resolution is
 * the one that counts.
 */
export function GrantResetCardDialog(props: Props) {
  const { t } = useTranslation()
  const { triggerRefresh } = useSubscriptions()
  const [draft, setDraft] = useState<ResetCardGrantDraft>(
    EMPTY_RESET_CARD_GRANT_DRAFT
  )
  const [lastResult, setLastResult] =
    useState<SubscriptionResetCardGrantResult | null>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)

  // Shared with the recipient fields through the query key, so this does not
  // fire a second request.
  const preview = useResetCardRecipientPreview(draft)

  const grant = useMutation({
    mutationFn: async () => {
      const result = requireServerSuccess(
        await grantSubscriptionResetCards(resetCardGrantPayload(draft))
      )
      return result.data as SubscriptionResetCardGrantResult
    },
    onSuccess: (result) => {
      setLastResult(result)
      const keys = resetCardGrantKeys(result.items)
      if (result.failed > 0) {
        toast.error(
          t('{{succeeded}} granted, {{failed}} failed', {
            succeeded: result.succeeded,
            failed: result.failed,
          })
        )
      } else if (draft.recipientMode === 'codes') {
        toast.success(
          t('Generated {{count}} redeem code(s)', { count: keys.length })
        )
      } else {
        toast.success(
          t('Granted {{count}} reset cards to {{users}} users', {
            count: result.succeeded * result.count,
            users: result.succeeded,
          })
        )
      }
      triggerRefresh()
      // A partial failure keeps the form visible so the operator can retry the
      // recipients that failed. A clean run swaps to the result view, where the
      // keys can be exported before the dialog closes — for codes those keys are
      // the whole deliverable.
      if (result.failed === 0) {
        setDraft(EMPTY_RESET_CARD_GRANT_DRAFT)
      }
    },
    onError: (error) => {
      handleServerError(error, t('Failed to grant reset cards'))
    },
  })

  const complete = isResetCardGrantDraftComplete(draft)
  const canSubmit = complete && !preview.overLimit

  const totalCards = useMemo(() => {
    if (draft.recipientMode === 'users') {
      return draft.userIds.length * draft.count
    }
    if (draft.recipientMode === 'codes') return draft.count
    // filter / all: the exact recipient set is only known server-side.
    return (preview.total ?? 0) * draft.count
  }, [draft.recipientMode, draft.userIds.length, draft.count, preview.total])

  const failedItems = (lastResult?.items ?? []).filter((item) => !item.ok)
  const grantedKeys = resetCardGrantKeys(lastResult?.items)
  const showResult = lastResult != null && failedItems.length === 0

  const submit = () => {
    setLastResult(null)
    // "All users" is the one mode whose blast radius an operator cannot see in
    // the form, so it gets an explicit acknowledgement before the request.
    if (draft.recipientMode === 'all') {
      setConfirmOpen(true)
      return
    }
    grant.mutate()
  }

  const close = () => {
    setDraft(EMPTY_RESET_CARD_GRANT_DRAFT)
    setLastResult(null)
    props.onOpenChange(false)
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={(open) => {
          if (!open && grant.isPending) return
          if (!open) {
            setDraft(EMPTY_RESET_CARD_GRANT_DRAFT)
            setLastResult(null)
          }
          props.onOpenChange(open)
        }}
        title={t('Grant reset cards')}
        description={t(
          'Each card carries this configuration until it is redeemed. Every recipient gets the configured number of independent cards.'
        )}
        contentHeight='min(72vh, 760px)'
        footer={
          <>
            <Button
              variant='outline'
              disabled={grant.isPending}
              onClick={() => (showResult ? close() : props.onOpenChange(false))}
            >
              {showResult ? t('Close') : t('Cancel')}
            </Button>
            {showResult ? (
              <Button
                onClick={() =>
                  downloadResetCardKeys(grantedKeys, 'reset-card-keys')
                }
                disabled={grantedKeys.length === 0}
              >
                <Download className='mr-1 h-4 w-4' />
                {t('Export {{count}} key(s)', { count: grantedKeys.length })}
              </Button>
            ) : (
              <Button
                disabled={!canSubmit || grant.isPending}
                onClick={submit}
              >
                {grant.isPending
                  ? t('Granting...')
                  : totalCards > 0
                    ? t('Grant {{count}} cards', { count: totalCards })
                    : t('Grant cards')}
              </Button>
            )}
          </>
        }
      >
        {showResult ? (
          <div className='flex flex-col gap-2'>
            <p className='text-sm font-medium'>
              {draft.recipientMode === 'codes'
                ? t('Generated {{count}} redeem code(s)', {
                    count: grantedKeys.length,
                  })
                : t('Granted {{count}} reset cards to {{users}} users', {
                    count: (lastResult?.succeeded ?? 0) * (lastResult?.count ?? 1),
                    users: lastResult?.succeeded ?? 0,
                  })}
            </p>
            <p className='text-muted-foreground text-xs'>
              {draft.recipientMode === 'codes'
                ? t(
                    'Export the codes and hand them out in your activity. Anyone signed in can redeem one.'
                  )
                : t(
                    'Export the keys to distribute them, or find them later in the reset card list.'
                  )}
            </p>
            {grantedKeys.length > 0 ? (
              <div className='bg-muted/40 max-h-64 overflow-y-auto rounded-lg border p-3 font-mono text-xs'>
                {grantedKeys.map((key) => (
                  <div key={key} className='break-all'>
                    {key}
                  </div>
                ))}
              </div>
            ) : null}
          </div>
        ) : (
          <ResetCardGrantFields
            draft={draft}
            onChange={setDraft}
            disabled={grant.isPending}
          />
        )}

        {failedItems.length > 0 ? (
          <div className='border-destructive/40 bg-destructive/5 mt-4 rounded-lg border p-3'>
            <p className='text-destructive text-sm font-medium'>
              {t('Some recipients failed')}
            </p>
            <ul className='text-muted-foreground mt-1 flex flex-col gap-0.5 text-xs'>
              {failedItems.map((item) => (
                <li key={item.user_id}>
                  ID {item.user_id}: {item.message || t('Unknown error')}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </Dialog>

      <RiskAcknowledgementDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Grant reset cards to all users')}
        description={t(
          'This mints cards for every enabled user in one batch. Narrow the audience with a filter if that is not what you meant.'
        )}
        items={[
          t('{{count}} user(s) will receive cards', {
            count: preview.total ?? 0,
          }),
          t('{{count}} card(s) will be created in total', {
            count: totalCards,
          }),
          t('Every recipient gets {{count}} card(s)', { count: draft.count }),
        ]}
        checklist={[
          t(
            'I understand these cards are granted to every enabled user and cannot be recalled in bulk.'
          ),
        ]}
        confirmText={t('Grant {{count}} cards', { count: totalCards })}
        isLoading={grant.isPending}
        onConfirm={() => {
          setConfirmOpen(false)
          grant.mutate()
        }}
      />
    </>
  )
}
