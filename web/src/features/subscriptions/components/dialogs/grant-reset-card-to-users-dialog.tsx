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
import { ResetCardConfigFields } from '../reset-card-config-fields'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Users the cards are granted to. The caller owns the table selection. */
  users: Array<{ id: number; username?: string }>
  onSuccess?: () => void
}

/**
 * Grants reset cards to the users selected in the users table. This is the
 * "pick the rows, then act" path: recipients are already known, so only the
 * card configuration is asked for.
 */
export function GrantResetCardToUsersDialog(props: Props) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<ResetCardGrantDraft>({
    ...EMPTY_RESET_CARD_GRANT_DRAFT,
    recipientMode: 'users',
  })
  const [lastResult, setLastResult] =
    useState<SubscriptionResetCardGrantResult | null>(null)

  const grant = useMutation({
    mutationFn: async () => {
      const result = requireServerSuccess(
        await grantSubscriptionResetCards(
          resetCardGrantPayload({
            ...draft,
            userIds: props.users.map((user) => user.id),
          })
        )
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
      if (result.failed === 0) {
        setDraft({ ...EMPTY_RESET_CARD_GRANT_DRAFT, recipientMode: 'users' })
        props.onSuccess?.()
      }
    },
    onError: (error) => {
      handleServerError(error, t('Failed to grant reset cards'))
    },
  })

  const complete =
    props.users.length > 0 &&
    isResetCardGrantDraftComplete({
      ...draft,
      userIds: props.users.map((user) => user.id),
    })
  const totalCards = useMemo(
    () => props.users.length * draft.count,
    [props.users.length, draft.count]
  )
  const failedItems = (lastResult?.items ?? []).filter((item) => !item.ok)
  const grantedKeys = resetCardGrantKeys(lastResult?.items)
  const showResult = lastResult != null && failedItems.length === 0

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open && grant.isPending) return
        if (!open) {
          setDraft({ ...EMPTY_RESET_CARD_GRANT_DRAFT, recipientMode: 'users' })
          setLastResult(null)
        }
        props.onOpenChange(open)
      }}
      title={t('Grant reset cards')}
      description={t(
        'Each selected user receives the configured number of independent cards, all carrying this configuration.'
      )}
      contentHeight='min(72vh, 760px)'
      footer={
        <>
          <Button
            variant='outline'
            disabled={grant.isPending}
            onClick={() => props.onOpenChange(false)}
          >
            {showResult ? t('Close') : t('Cancel')}
          </Button>
          {showResult ? (
            <Button
              onClick={() => downloadResetCardKeys(grantedKeys, 'reset-card-keys')}
              disabled={grantedKeys.length === 0}
            >
              <Download className='mr-1 h-4 w-4' />
              {t('Export {{count}} key(s)', { count: grantedKeys.length })}
            </Button>
          ) : (
            <Button disabled={!complete || grant.isPending} onClick={() => grant.mutate()}>
              {grant.isPending
                ? t('Granting...')
                : t('Grant {{count}} cards', { count: totalCards })}
            </Button>
          )}
        </>
      }
    >
      {showResult ? (
        <div className='flex flex-col gap-2'>
          <p className='text-sm font-medium'>
            {t('Granted {{count}} reset cards to {{users}} users', {
              count: (lastResult?.succeeded ?? 0) * (lastResult?.count ?? 1),
              users: lastResult?.succeeded ?? 0,
            })}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
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
        <div className='flex flex-col gap-4'>
          <div className='text-muted-foreground flex flex-col gap-1 text-xs'>
            <span>
              {t('Selected users: {{count}}', { count: props.users.length })}
            </span>
            {props.users.length > 0 ? (
              <span className='truncate'>
                {props.users
                  .map((user) => user.username || `#${user.id}`)
                  .join(', ')}
              </span>
            ) : null}
          </div>
          <ResetCardConfigFields
            draft={draft}
            onChange={setDraft}
            disabled={grant.isPending}
            // A subscription belongs to one person; a multi-user grant has no
            // way to name which one.
            blockedScopes={['subscription']}
          />
        </div>
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
  )
}
