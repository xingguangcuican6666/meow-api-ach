/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { handleServerError } from '@/lib/handle-server-error'

import { resetPlanSubscriptions } from '../../api'
import {
  DEFAULT_SUBSCRIPTION_RESET_SCOPE,
  type SubscriptionResetScopeValue,
} from '../../lib/reset-scope'
import { SubscriptionResetScopeFields } from '../subscription-reset-scope-fields'
import { useSubscriptions } from '../subscriptions-provider'

export function ResetSubscriptionsDialog() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, triggerRefresh } = useSubscriptions()
  const [resetScope, setResetScope] = useState<SubscriptionResetScopeValue>(
    DEFAULT_SUBSCRIPTION_RESET_SCOPE
  )
  const [resetting, setResetting] = useState(false)
  const isOpen = open === 'reset-subscriptions'
  const plan = currentRow?.plan
  const planLabel = plan?.title || (plan?.id ? `#${plan.id}` : '-')

  useEffect(() => {
    if (isOpen) setResetScope(DEFAULT_SUBSCRIPTION_RESET_SCOPE)
  }, [isOpen])

  const handleConfirm = async () => {
    if (!plan?.id) return
    setResetting(true)
    try {
      const res = await resetPlanSubscriptions(plan.id, { ...resetScope })
      if (res.success) {
        toast.success(
          t('Reset {{count}} active subscriptions', {
            count: res.data?.reset_count || 0,
          })
        )
        triggerRefresh()
        setOpen(null)
      } else {
        handleServerError(res)
      }
    } catch (error) {
      handleServerError(error, t('Operation failed'))
    } finally {
      setResetting(false)
    }
  }

  return (
    <ConfirmDialog
      open={isOpen}
      onOpenChange={(nextOpen) => !nextOpen && setOpen(null)}
      title={t('Reset subscription quota')}
      desc={t('Reset all active subscriptions under {{plan}}?', {
        plan: planLabel,
      })}
      confirmText={t('Reset quota')}
      handleConfirm={handleConfirm}
      disabled={!plan?.id}
      isLoading={resetting}
    >
      <SubscriptionResetScopeFields
        value={resetScope}
        onChange={setResetScope}
        disabled={resetting}
      />
    </ConfirmDialog>
  )
}
