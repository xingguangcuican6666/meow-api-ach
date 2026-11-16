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

/**
 * The reset selection every reset surface shares. `advance_reset_time` moves the
 * next scheduled reset along the plan's existing cycle; the other switches pick
 * which counters are zeroed. Rolling 5h/7d windows are off by default because a
 * manual reset should not shorten a window the user already earned.
 */
export interface SubscriptionResetScopeValue {
  advance_reset_time: boolean
  reset_amount_used: boolean
  reset_cycle_limits: boolean
  reset_window_limits: boolean
}

export const DEFAULT_SUBSCRIPTION_RESET_SCOPE: SubscriptionResetScopeValue = {
  advance_reset_time: true,
  reset_amount_used: true,
  reset_cycle_limits: true,
  reset_window_limits: false,
}
