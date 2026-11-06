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
import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { handleServerError } from '@/lib/handle-server-error'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { batchGrantSubscriptions, getAdminPlans } from '../../api'
import type { SubscriptionBatchResult } from '../../types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Users the plan is granted to. The caller owns the selection. */
  users: Array<{ id: number; username?: string }>
  onSuccess?: () => void
}

/**
 * Grants one subscription plan to every selected user. Batch grant runs one
 * transaction per user server-side, so a user who already hit the plan's
 * purchase limit fails alone while the rest still succeed; the result is
 * reported with those counts rather than as a single success or failure.
 */
export function GrantPlanDialog(props: Props) {
  const { t } = useTranslation()
  const [planId, setPlanId] = useState<string | null>(null)

  const { data: planOptions = [], isLoading: plansLoading } = useQuery({
    queryKey: ['admin-subscription-plans', 'grant-options'],
    queryFn: async () => {
      const result = requireServerSuccess(await getAdminPlans())
      return (result.data || [])
        .filter((record) => record.plan.enabled)
        .map((record) => ({
          value: String(record.plan.id),
          label: `${record.plan.title} ($${Number(record.plan.price_amount || 0).toFixed(2)})`,
        }))
    },
    enabled: props.open,
    staleTime: 0,
  })

  const grant = useMutation({
    mutationFn: async (options: { userIds: number[]; planId: number }) => {
      const result = await batchGrantSubscriptions({
        user_ids: options.userIds,
        plan_id: options.planId,
      })
      if (!result.success) throw createServerError(result)
      return result.data as SubscriptionBatchResult
    },
    onSuccess: (result) => {
      if (result.failed > 0) {
        toast.error(
          t('{{succeeded}} granted, {{failed}} failed', {
            succeeded: result.succeeded,
            failed: result.failed,
          })
        )
      } else {
        toast.success(
          t('Granted a subscription to {{count}} users', {
            count: result.succeeded,
          })
        )
      }
      setPlanId(null)
      props.onOpenChange(false)
      props.onSuccess?.()
    },
    onError: (error) => {
      handleServerError(error, t('Failed to grant subscriptions'))
    },
  })

  return (
    <Dialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open && grant.isPending) return
        if (!open) setPlanId(null)
        props.onOpenChange(open)
      }}
      title={t('Grant a subscription plan')}
      description={t(
        'The selected plan is added to each chosen user as a new subscription, exactly like the per-user admin action.'
      )}
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
            disabled={!planId || props.users.length === 0 || grant.isPending}
            onClick={() => {
              if (!planId) return
              grant.mutate({
                userIds: props.users.map((user) => user.id),
                planId: Number.parseInt(planId, 10),
              })
            }}
          >
            {grant.isPending
              ? t('Granting...')
              : t('Grant to {{count}} users', { count: props.users.length })}
          </Button>
        </>
      }
    >
      <div className='flex flex-col gap-3'>
        <label className='text-sm font-medium'>{t('Subscription plan')}</label>
        <Combobox
          options={planOptions}
          value={planId}
          onValueChange={(value) => setPlanId(value)}
          placeholder={
            plansLoading ? t('Loading...') : t('Select subscription plan')
          }
          disabled={plansLoading || planOptions.length === 0}
          className='w-full'
        />
        {planOptions.length === 0 && !plansLoading ? (
          <p className='text-muted-foreground text-xs'>
            {t('No enabled subscription plan exists yet.')}
          </p>
        ) : null}
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
      </div>
    </Dialog>
  )
}
