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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { grantSubscriptionResetCards } from '../../api'
import {
  EMPTY_RESET_CARD_GRANT_DRAFT,
  isResetCardGrantDraftComplete,
  resetCardGrantPayload,
  type ResetCardGrantDraft,
} from '../../lib/reset-card-grant'
import type { SubscriptionResetCardGrantResult } from '../../types'
import { ResetCardGrantFields } from '../reset-card-grant-fields'
import { useSubscriptions } from '../subscriptions-provider'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Root's grant form. One submission mints `count` independent cards for every
 * chosen recipient, all sharing the same frozen configuration and batch id —
 * the "n cards of this configuration to n users" case.
 */
export function GrantResetCardDialog(props: Props) {
  const { t } = useTranslation()
  const { triggerRefresh } = useSubscriptions()
  const [draft, setDraft] = useState<ResetCardGrantDraft>(
    EMPTY_RESET_CARD_GRANT_DRAFT
  )
  const [lastResult, setLastResult] =
    useState<SubscriptionResetCardGrantResult | null>(null)

  const grant = useMutation({
    mutationFn: async () => {
      const result = requireServerSuccess(
        await grantSubscriptionResetCards(resetCardGrantPayload(draft))
      )
      return result.data as SubscriptionResetCardGrantResult
    },
    onSuccess: (result) => {
      setLastResult(result)
      if (result.failed > 0) {
        toast.error(
          t('{{succeeded}} granted, {{failed}} failed', {
            succeeded: result.succeeded,
            failed: result.failed,
          })
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
      // A partial failure keeps the dialog open so the operator can see which
      // recipients failed and retry just those.
      if (result.failed === 0) {
        setDraft(EMPTY_RESET_CARD_GRANT_DRAFT)
        props.onOpenChange(false)
      }
    },
    onError: (error) => {
      handleServerError(error, t('Failed to grant reset cards'))
    },
  })

  const complete = isResetCardGrantDraftComplete(draft)
  const totalCards = useMemo(
    () => draft.userIds.length * draft.count,
    [draft.userIds.length, draft.count]
  )

  const failedItems = (lastResult?.items ?? []).filter((item) => !item.ok)

  return (
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
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            disabled={!complete || grant.isPending}
            onClick={() => {
              setLastResult(null)
              grant.mutate()
            }}
          >
            {grant.isPending
              ? t('Granting...')
              : t('Grant {{count}} cards', { count: totalCards })}
          </Button>
        </>
      }
    >
      <ResetCardGrantFields
        draft={draft}
        onChange={setDraft}
        disabled={grant.isPending}
      />

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
  )
}
