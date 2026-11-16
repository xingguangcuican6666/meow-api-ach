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
  SubscriptionResetCardScope,
} from '../types'
import {
  DEFAULT_SUBSCRIPTION_RESET_SCOPE,
  type SubscriptionResetScopeValue,
} from './reset-scope'

/** The editable draft a caller holds before submitting a grant. */
export interface ResetCardGrantDraft {
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
 * Turns a draft into the wire payload. Only the fields the selected scope uses
 * are sent, so a plan id left over from a previous scope never reaches the
 * server.
 */
export function resetCardGrantPayload(
  draft: ResetCardGrantDraft
): GrantSubscriptionResetCardsRequest {
  return {
    user_ids: draft.userIds,
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

/** Whether a draft carries everything the server requires. */
export function isResetCardGrantDraftComplete(
  draft: ResetCardGrantDraft
): boolean {
  if (draft.userIds.length === 0) return false
  if (draft.count < 1) return false
  if (draft.targetScope === 'plan' && !draft.planId) return false
  if (draft.targetScope === 'subscription' && !draft.userSubscriptionId) {
    return false
  }
  return (
    draft.scope.reset_amount_used ||
    draft.scope.reset_cycle_limits ||
    draft.scope.reset_window_limits
  )
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
