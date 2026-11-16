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
import { z } from 'zod'

// ============================================================================
// Subscription Plan Schema & Types
// ============================================================================

export const subscriptionQuotaLimitSchema = z.object({
  key: z.string().optional(),
  name: z.string().optional(),
  window_seconds: z.number(),
  amount_total: z.number(),
  models: z.array(z.string()).optional(),
})

export type SubscriptionQuotaLimit = z.infer<
  typeof subscriptionQuotaLimitSchema
>

export const subscriptionLimitUsageSchema = z.object({
  key: z.string(),
  name: z.string().optional(),
  window_seconds: z.number(),
  amount_total: z.number(),
  models: z.array(z.string()).optional(),
  model: z.string().optional(),
  amount_used: z.number(),
  amount_remain: z.number(),
  window_from: z.number().optional(),
  next_reset_time: z.number().optional(),
})

export type SubscriptionLimitUsage = z.infer<
  typeof subscriptionLimitUsageSchema
>

export const subscriptionPlanSchema = z.object({
  id: z.number(),
  title: z.string(),
  subtitle: z.string().optional(),
  price_amount: z.number(),
  currency: z.string().default('USD'),
  duration_unit: z.enum(['year', 'month', 'day', 'hour', 'custom']),
  duration_value: z.number(),
  custom_seconds: z.number().optional(),
  quota_reset_period: z.enum(['never', 'daily', 'weekly', 'monthly', 'custom']),
  quota_reset_custom_seconds: z.number().optional(),
  enabled: z.boolean(),
  sort_order: z.number(),
  allow_balance_pay: z.boolean().optional().default(true),
  allow_wallet_overflow: z.boolean().optional().default(true),
  max_purchase_per_user: z.number(),
  total_amount: z.number(),
  /**
   * Whitelist of models this plan's subscription quota may be spent on.
   * Absent or empty means every model, which is how plans behaved before the
   * field existed. A model outside the list never draws down this plan.
   */
  allowed_models: z.array(z.string()).optional(),
  quota_limits: z.array(subscriptionQuotaLimitSchema).optional(),
  upgrade_group: z.string().optional(),
  downgrade_group: z.string().optional(),
  stripe_price_id: z.string().optional(),
  creem_product_id: z.string().optional(),
  waffo_pancake_product_id: z.string().optional(),
})

export type SubscriptionPlan = z.infer<typeof subscriptionPlanSchema>

export interface PlanRecord {
  plan: SubscriptionPlan
}

// ============================================================================
// User Subscription Schema & Types
// ============================================================================

export const userSubscriptionSchema = z.object({
  id: z.number(),
  user_id: z.number(),
  plan_id: z.number(),
  status: z.string(),
  source: z.string().optional(),
  start_time: z.number(),
  end_time: z.number(),
  amount_total: z.number(),
  amount_used: z.number(),
  next_reset_time: z.number().optional(),
})

export type UserSubscription = z.infer<typeof userSubscriptionSchema>

export interface UserSubscriptionRecord {
  subscription: UserSubscription
  /** Caps configured on the plan with this subscription's counters. */
  quota_limits?: SubscriptionLimitUsage[]
  /**
   * The plan's model whitelist. Absent means the plan covers every model; a
   * present list means only those models may spend this subscription.
   */
  allowed_models?: string[]
}

/**
 * One row of the cross-user admin subscription list. The owner's username and
 * the plan title are resolved server-side so the table needs no per-row lookup.
 */
export interface SubscriptionListItem extends UserSubscriptionRecord {
  user_id: number
  username: string
  plan_id: number
  plan_title: string
}

// ============================================================================
// Cross-user Subscription Management
// ============================================================================

export const SUBSCRIPTION_STATUS_VALUES = [
  'active',
  'expired',
  'cancelled',
] as const

export type SubscriptionStatusValue =
  (typeof SUBSCRIPTION_STATUS_VALUES)[number]

export interface ListSubscriptionsParams {
  p?: number
  page_size?: number
  keyword?: string
  status?: string
  plan_id?: number
  user_id?: number
  active_only?: boolean
  /** Server-side ordering; the column whitelist is enforced by the backend. */
  sort_by?: SubscriptionSortBy
  sort_order?: SubscriptionSortOrder
}

export type SubscriptionSortBy =
  | 'id'
  | 'user_id'
  | 'plan_id'
  | 'status'
  | 'amount_total'
  | 'amount_used'
  | 'start_time'
  | 'end_time'
  | 'next_reset_time'
  | 'created_at'
  | 'updated_at'

export type SubscriptionSortOrder = 'asc' | 'desc'

export interface ListSubscriptionsResponse {
  success: boolean
  message?: string
  data?: {
    page: number
    page_size: number
    total: number
    items: SubscriptionListItem[]
  }
}

/** What happened to one entry of a batch operation. */
export interface SubscriptionBatchItemOutcome {
  id?: number
  user_id?: number
  /** Set on a grant entry, where the target is a user rather than a row. */
  plan_id?: number
  ok: boolean
  message?: string
}

export interface SubscriptionBatchResult {
  requested: number
  succeeded: number
  failed: number
  items: SubscriptionBatchItemOutcome[]
}

export interface BatchGrantSubscriptionsRequest {
  user_ids: number[]
  plan_id: number
}

export interface BatchResetSubscriptionsRequest {
  ids: number[]
  advance_reset_time: boolean
  reset_amount_used?: boolean
  reset_cycle_limits?: boolean
  reset_window_limits?: boolean
  limit_keys?: string[]
}

// ============================================================================
// API Request/Response Types
// ============================================================================

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface PlanPayload {
  plan: Partial<SubscriptionPlan>
}

export interface SubscriptionPayRequest {
  plan_id: number
  payment_method?: string
}

export interface SubscriptionPayResponse {
  success: boolean
  message?: string
  data?: {
    // Stripe-style hosted checkout link.
    pay_link?: string
    // Waffo Pancake / Creem hosted checkout URL.
    checkout_url?: string
    // Pancake-only: order metadata + self-service buyer session token,
    // surfaced for future flows (refund / cancel from new-api's own UI).
    session_id?: string
    expires_at?: number | string
    order_id?: string
    token?: string
    token_expires_at?: number | string
  }
  url?: string
}

export interface CreateUserSubscriptionRequest {
  plan_id: number
}

export interface ResetUserSubscriptionsRequest {
  plan_id: number
  advance_reset_time: boolean
  reset_amount_used?: boolean
  reset_cycle_limits?: boolean
  reset_window_limits?: boolean
  limit_keys?: string[]
}

export interface ResetPlanSubscriptionsRequest {
  advance_reset_time: boolean
  reset_amount_used?: boolean
  reset_cycle_limits?: boolean
  reset_window_limits?: boolean
  limit_keys?: string[]
}

export interface SubscriptionResetResult {
  plan_id: number
  matched_count: number
  reset_count: number
  user_count: number
  advance_reset_time: boolean
  reset_amount_used: boolean
  reset_cycle_limits: boolean
  reset_window_limits: boolean
  limit_keys?: string[]
}

// ============================================================================
// Subscription Reset Cards
// ============================================================================

export const SUBSCRIPTION_RESET_CARD_SCOPES = [
  'all',
  'plan',
  'subscription',
] as const

export type SubscriptionResetCardScope =
  (typeof SUBSCRIPTION_RESET_CARD_SCOPES)[number]

export const SUBSCRIPTION_RESET_CARD_STATUSES = [
  'unused',
  'used',
  'disabled',
] as const

export type SubscriptionResetCardStatus =
  (typeof SUBSCRIPTION_RESET_CARD_STATUSES)[number]

/**
 * A one-shot voucher that resets subscription counters. The reset selection is
 * frozen on the card when root grants it; the target list is resolved when the
 * holder redeems it.
 */
export interface SubscriptionResetCard {
  id: number
  user_id: number
  name: string
  key: string
  status: SubscriptionResetCardStatus
  batch_id?: string
  target_scope: SubscriptionResetCardScope
  plan_id?: number
  user_subscription_id?: number
  limit_keys?: string[]
  reset_amount_used: boolean
  reset_cycle_limits: boolean
  reset_window_limits: boolean
  advance_reset_time: boolean
  expired_time?: number
  used_time?: number
  used_by_user_id?: number
  created_time?: number
  updated_time?: number
}

/** One row of the root card list, with the holder's username resolved. */
export interface SubscriptionResetCardListItem {
  card: SubscriptionResetCard
  username: string
}

/** The reset selection shared by card grants and manual resets. */
export interface SubscriptionResetScopePayload {
  reset_amount_used: boolean
  reset_cycle_limits: boolean
  reset_window_limits: boolean
  limit_keys?: string[]
}

export interface GrantSubscriptionResetCardsRequest extends SubscriptionResetScopePayload {
  user_ids: number[]
  count: number
  name?: string
  expired_time?: number
  target_scope: SubscriptionResetCardScope
  plan_id?: number
  user_subscription_id?: number
  advance_reset_time: boolean
}

export interface SubscriptionResetCardGrantItem {
  user_id: number
  ok: boolean
  message?: string
  card_ids?: number[]
  keys?: string[]
}

export interface SubscriptionResetCardGrantResult {
  requested: number
  succeeded: number
  failed: number
  count: number
  batch_id: string
  items: SubscriptionResetCardGrantItem[]
}

export interface ListSubscriptionResetCardsParams {
  p?: number
  page_size?: number
  keyword?: string
  status?: string
  user_id?: number
  batch_id?: string
}

export interface RedeemSubscriptionResetCardRequest {
  card_id?: number
  key?: string
}

export interface SubscriptionResetCardUseResult {
  card_id: number
  name: string
  target_scope: SubscriptionResetCardScope
  plan_id?: number
  user_subscription_id?: number
  matched_count: number
  reset_count: number
  advance_reset_time: boolean
  reset_amount_used: boolean
  reset_cycle_limits: boolean
  reset_window_limits: boolean
  limit_keys?: string[]
}

// ============================================================================
// Self Subscription Data (user-facing)
// ============================================================================

export interface SelfSubscriptionData {
  billing_preference: string
  subscriptions: UserSubscriptionRecord[]
  all_subscriptions: UserSubscriptionRecord[]
}

// ============================================================================
// Dialog Types
// ============================================================================

export type SubscriptionsDialogType =
  | 'create'
  | 'update'
  | 'toggle-status'
  | 'reset-subscriptions'
