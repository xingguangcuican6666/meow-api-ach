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
import { type TFunction } from 'i18next'

// ============================================================================
// Duration Unit Options
// ============================================================================

export const DURATION_UNITS = [
  { value: 'year', labelKey: 'years' },
  { value: 'month', labelKey: 'months' },
  { value: 'day', labelKey: 'days' },
  { value: 'hour', labelKey: 'hours' },
  { value: 'custom', labelKey: 'Custom (seconds)' },
] as const

export const RESET_PERIODS = [
  { value: 'never', labelKey: 'No Reset' },
  { value: 'daily', labelKey: 'Daily' },
  { value: 'weekly', labelKey: 'Weekly' },
  { value: 'monthly', labelKey: 'Monthly' },
  { value: 'custom', labelKey: 'Custom (seconds)' },
] as const

// Rolling windows an operator can attach to an extra quota limit. The seconds
// match model.SubscriptionLimitWindow5h / SubscriptionLimitWindow7d.
export const QUOTA_LIMIT_WINDOWS = [
  { value: 0, labelKey: 'Whole subscription period' },
  { value: 5 * 60 * 60, labelKey: '5 hours' },
  { value: 7 * 24 * 60 * 60, labelKey: '7 days' },
  { value: 60 * 60, labelKey: '1 hour' },
  { value: 24 * 60 * 60, labelKey: '1 day' },
  { value: 30 * 24 * 60 * 60, labelKey: '30 days' },
] as const

export function getDurationUnitOptions(t: TFunction) {
  return DURATION_UNITS.map((u) => ({ value: u.value, label: t(u.labelKey) }))
}

export function getResetPeriodOptions(t: TFunction) {
  return RESET_PERIODS.map((p) => ({ value: p.value, label: t(p.labelKey) }))
}

export function getQuotaLimitWindowOptions(t: TFunction) {
  return QUOTA_LIMIT_WINDOWS.map((w) => ({
    value: String(w.value),
    label: t(w.labelKey),
  }))
}
