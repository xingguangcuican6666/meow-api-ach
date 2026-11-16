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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { RotateCcw, Ticket } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import {
  getSelfSubscriptionResetCards,
  redeemSubscriptionResetCard,
} from '@/features/subscriptions/api'
import { formatTimestamp } from '@/features/subscriptions/lib'
import type { SubscriptionResetCard } from '@/features/subscriptions/types'
import { handleServerError } from '@/lib/handle-server-error'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

/**
 * One card's reset selection, rendered as a sentence. The configuration was
 * frozen by root at grant time, so this is purely a description of the card.
 */
function cardScopeSummary(card: SubscriptionResetCard, t: TFunction): string {
  const parts: string[] = []
  if (card.reset_amount_used) parts.push(t('total quota usage'))
  if (card.reset_cycle_limits) parts.push(t('cycle limit counters'))
  if (card.reset_window_limits) parts.push(t('rolling window counters'))

  let scope: string
  switch (card.target_scope) {
    case 'plan':
      scope = t('plan #{{id}}', { id: card.plan_id ?? '-' })
      break
    case 'subscription':
      scope = t('subscription #{{id}}', {
        id: card.user_subscription_id ?? '-',
      })
      break
    default:
      scope = t('all active subscriptions')
  }

  if (parts.length === 0) return scope
  return t('{{scope}}: {{parts}}', { scope, parts: parts.join(', ') })
}

function isExpired(card: SubscriptionResetCard): boolean {
  return (
    Boolean(card.expired_time) && Number(card.expired_time) * 1000 < Date.now()
  )
}

/**
 * The holder's side of the reset-card system: every unused card root granted to
 * this user, each redeemable independently. Cards disappear from this list once
 * spent, so what remains is always actionable.
 */
export function ResetCardsCard() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ['self-subscription-reset-cards'],
    queryFn: async () => {
      const result = requireServerSuccess(await getSelfSubscriptionResetCards())
      return result.data || []
    },
    staleTime: 30_000,
  })

  const redeem = useMutation({
    mutationFn: async (cardId: number) => {
      const result = await redeemSubscriptionResetCard({ card_id: cardId })
      if (!result.success) throw createServerError(result)
      return result.data
    },
    onSuccess: (result) => {
      toast.success(
        t('Reset {{count}} subscriptions', { count: result?.reset_count ?? 0 })
      )
      void queryClient.invalidateQueries({
        queryKey: ['self-subscription-reset-cards'],
      })
      // Subscription usage shown elsewhere on the page is now stale.
      void queryClient.invalidateQueries({ queryKey: ['self-subscription'] })
    },
    onError: (error) => {
      handleServerError(error, t('Failed to redeem reset card'))
    },
  })

  const cards = (data || []).filter((card) => card.status === 'unused')
  // A user with no cards never sees the section; there is nothing to explain.
  if (!isLoading && cards.length === 0) return null

  return (
    <TitledCard
      title={t('Subscription Reset Cards')}
      description={t(
        'Each card resets the subscription counters root configured when it was granted. Cards are single use.'
      )}
      icon={<Ticket className='h-4 w-4' />}
      iconTone='info'
    >
      {isLoading ? (
        <div className='flex flex-col gap-2 p-4'>
          <Skeleton className='h-10 w-full' />
          <Skeleton className='h-10 w-full' />
        </div>
      ) : (
        <ul className='divide-y'>
          {cards.map((card) => {
            const expired = isExpired(card)
            return (
              <li
                key={card.id}
                className='flex flex-col gap-2 p-3 sm:flex-row sm:items-center sm:justify-between sm:p-4'
              >
                <div className='min-w-0'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='truncate font-medium'>
                      {card.name || t('Reset card')}
                    </span>
                    {expired ? (
                      <StatusBadge
                        label={t('Expired')}
                        variant='warning'
                        copyable={false}
                      />
                    ) : null}
                  </div>
                  <p className='text-muted-foreground mt-0.5 text-xs'>
                    {cardScopeSummary(card, t)}
                  </p>
                  {card.expired_time ? (
                    <p className='text-muted-foreground text-xs'>
                      {t('Expires At')}: {formatTimestamp(card.expired_time)}
                    </p>
                  ) : null}
                </div>
                <Button
                  size='sm'
                  variant='outline'
                  className='shrink-0'
                  disabled={expired || redeem.isPending}
                  onClick={() => redeem.mutate(card.id)}
                >
                  <RotateCcw className='mr-1 h-4 w-4' />
                  {redeem.isPending ? t('Resetting...') : t('Use card')}
                </Button>
              </li>
            )
          })}
        </ul>
      )}
    </TitledCard>
  )
}
