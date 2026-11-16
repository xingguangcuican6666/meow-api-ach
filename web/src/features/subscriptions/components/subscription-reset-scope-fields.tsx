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
import { useId } from 'react'
import { useTranslation } from 'react-i18next'

import { Switch } from '@/components/ui/switch'

import type { SubscriptionResetScopeValue } from '../lib/reset-scope'

interface Props {
  value: SubscriptionResetScopeValue
  onChange: (value: SubscriptionResetScopeValue) => void
  disabled?: boolean
}

/**
 * One labelled switch with its explanation beneath. The explanation is bound
 * through aria-describedby rather than placed inside the label, so the switch's
 * accessible name stays exactly the label text.
 */
function ScopeSwitch(props: {
  label: string
  description: string
  checked: boolean
  disabled?: boolean
  onChange: (checked: boolean) => void
}) {
  const switchId = useId()
  const descriptionId = useId()
  return (
    <div className='flex items-start justify-between gap-3 rounded-md border px-3 py-2 text-sm'>
      <div className='flex min-w-0 flex-col gap-0.5'>
        <label htmlFor={switchId} className='cursor-pointer'>
          {props.label}
        </label>
        <span id={descriptionId} className='text-muted-foreground text-xs'>
          {props.description}
        </span>
      </div>
      <Switch
        id={switchId}
        checked={props.checked}
        disabled={props.disabled}
        aria-describedby={descriptionId}
        onCheckedChange={(checked) => props.onChange(!!checked)}
      />
    </div>
  )
}

/** Shared reset-scope editor used by every reset dialog. */
export function SubscriptionResetScopeFields(props: Props) {
  const { t } = useTranslation()
  const set = (patch: Partial<SubscriptionResetScopeValue>) =>
    props.onChange({ ...props.value, ...patch })

  return (
    <div className='flex flex-col gap-2'>
      <ScopeSwitch
        label={t('Advance next reset time')}
        description={t(
          'Move the next scheduled reset along the plan cycle; the remaining time is kept'
        )}
        checked={props.value.advance_reset_time}
        disabled={props.disabled}
        onChange={(checked) => set({ advance_reset_time: checked })}
      />
      <ScopeSwitch
        label={t('Reset total quota usage')}
        description={t('Clear the whole-period usage counter')}
        checked={props.value.reset_amount_used}
        disabled={props.disabled}
        onChange={(checked) => set({ reset_amount_used: checked })}
      />
      <ScopeSwitch
        label={t('Reset cycle limit counters')}
        description={t(
          'Clear the extra limits that follow the plan reset cycle'
        )}
        checked={props.value.reset_cycle_limits}
        disabled={props.disabled}
        onChange={(checked) => set({ reset_cycle_limits: checked })}
      />
      <ScopeSwitch
        label={t('Reset rolling window counters')}
        description={t('Also clear running 5h/7d windows and start them fresh')}
        checked={props.value.reset_window_limits}
        disabled={props.disabled}
        onChange={(checked) => set({ reset_window_limits: checked })}
      />
    </div>
  )
}
