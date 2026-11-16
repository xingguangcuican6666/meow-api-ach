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
import type {
  GrantSubscriptionResetCardsRequest,
  ResetCardRecipientFilter,
  ResetCardRecipientMode,
  SubscriptionResetCardScope,
} from '../types'
import {
  DEFAULT_SUBSCRIPTION_RESET_SCOPE,
  type SubscriptionResetScopeValue,
} from './reset-scope'

/**
 * The recipient conditions an operator can set in filter mode. Empty strings
 * mean "do not filter on this field"; the server then defaults status to
 * enabled users.
 */
export interface ResetCardRecipientFilterDraft {
  keyword: string
  group: string
  role: string
  status: string
}

export const EMPTY_RESET_CARD_RECIPIENT_FILTER: ResetCardRecipientFilterDraft =
  {
    keyword: '',
    group: '',
    role: '',
    status: '',
  }

/** The editable draft a caller holds before submitting a grant. */
export interface ResetCardGrantDraft {
  recipientMode: ResetCardRecipientMode
  filters: ResetCardRecipientFilterDraft
  userIds: number[]
  count: number
  name: string
  expiredTime: number
  targetScope: SubscriptionResetCardScope
  planId: string | null
  userSubscriptionId: string
  scope: SubscriptionResetScopeValue
}

export const EMPTY_RESET_CARD_GRANT_DRAFT: ResetCardGrantDraft = {
  recipientMode: 'users',
  filters: EMPTY_RESET_CARD_RECIPIENT_FILTER,
  userIds: [],
  count: 1,
  name: '',
  expiredTime: 0,
  targetScope: 'all',
  planId: null,
  userSubscriptionId: '',
  scope: DEFAULT_SUBSCRIPTION_RESET_SCOPE,
}

/**
 * Turns the filter draft into the wire shape. Unset conditions are dropped
 * rather than sent as empty strings, so the server's defaults apply.
 */
export function recipientFilterPayload(
  filters: ResetCardRecipientFilterDraft
): ResetCardRecipientFilter {
  const role = Number.parseInt(filters.role, 10)
  const status = Number.parseInt(filters.status, 10)
  return {
    keyword: filters.keyword.trim() || undefined,
    group: filters.group.trim() || undefined,
    role: Number.isFinite(role) && role > 0 ? role : undefined,
    status: Number.isFinite(status) && status > 0 ? status : undefined,
  }
}

/**
 * Turns a draft into the wire payload. Only the fields the selected recipient
 * mode and target scope use are sent, so a stale plan id or user list from a
 * previous mode never reaches the server.
 */
export function resetCardGrantPayload(
  draft: ResetCardGrantDraft
): GrantSubscriptionResetCardsRequest {
  return {
    recipient_mode: draft.recipientMode,
    user_ids: draft.recipientMode === 'users' ? draft.userIds : undefined,
    filter:
      draft.recipientMode === 'filter'
        ? recipientFilterPayload(draft.filters)
        : undefined,
    count: draft.count,
    name: draft.name.trim() || undefined,
    expired_time: draft.expiredTime || undefined,
    target_scope: draft.targetScope,
    plan_id:
      draft.targetScope === 'plan' && draft.planId
        ? Number.parseInt(draft.planId, 10)
        : undefined,
    user_subscription_id:
      draft.targetScope === 'subscription' && draft.userSubscriptionId
        ? Number.parseInt(draft.userSubscriptionId, 10)
        : undefined,
    ...draft.scope,
  }
}

/**
 * Whether a draft carries everything the server requires. Only the explicit
 * user list is checked here: filter and all resolve their recipients on the
 * server, and codes have none.
 */
export function isResetCardGrantDraftComplete(
  draft: ResetCardGrantDraft
): boolean {
  if (draft.recipientMode === 'users' && draft.userIds.length === 0) return false
  if (draft.count < 1) return false
  if (draft.targetScope === 'plan' && !draft.planId) return false
  if (draft.targetScope === 'subscription' && !draft.userSubscriptionId) {
    return false
  }
  // A subscription belongs to one person, so only an explicit user list can
  // target it. The server enforces the same rule.
  if (
    draft.targetScope === 'subscription' &&
    draft.recipientMode !== 'users'
  ) {
    return false
  }
  return (
    draft.scope.reset_amount_used ||
    draft.scope.reset_cycle_limits ||
    draft.scope.reset_window_limits
  )
}

/** Whether the mode mints ownerless redeem codes rather than addressed cards. */
export function isResetCardCodeMode(draft: ResetCardGrantDraft): boolean {
  return draft.recipientMode === 'codes'
}

/**
 * Flattens a grant result into the keys an operator distributes. Ownerless
 * codes and addressed cards both export the same way: one key per line.
 */
export function resetCardGrantKeys(
  items: Array<{ keys?: string[] }> | undefined
): string[] {
  const keys: string[] = []
  for (const item of items ?? []) {
    for (const key of item.keys ?? []) {
      if (key) keys.push(key)
    }
  }
  return keys
}

/** Triggers a browser download of one key per line. */
export function downloadResetCardKeys(keys: string[], filenamePrefix: string) {
  if (keys.length === 0) return
  const blob = new Blob([`${keys.join('\n')}\n`], {
    type: 'text/plain;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `${filenamePrefix}-${Date.now()}.txt`
  document.body.append(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

/** Display labels for the four recipient modes. */
export const RESET_CARD_RECIPIENT_MODE_LABELS: Record<
  ResetCardRecipientMode,
  string
> = {
  users: 'Specific users',
  filter: 'Users matching a filter',
  all: 'All users',
  codes: 'Redeem codes',
}

/** Display labels for the three card target scopes. */
export const RESET_CARD_SCOPE_LABELS: Record<
  SubscriptionResetCardScope,
  string
> = {
  all: 'All active subscriptions',
  plan: 'Subscriptions of one plan',
  subscription: 'One specific subscription',
}
