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
import { createSectionRegistry } from '@/features/system-settings/utils/section-registry'

/**
 * Subscription management sections. `plans` is the existing plan catalog;
 * `subscriptions` is the cross-user list an administrator uses to grant,
 * reset, cancel or delete many subscriptions at once.
 */
const SUBSCRIPTIONS_SECTIONS = [
  {
    id: 'plans',
    titleKey: 'Subscription Plans',
    build: () => null, // Content is rendered directly in the page component
  },
  {
    id: 'subscriptions',
    titleKey: 'User Subscriptions',
    build: () => null,
  },
] as const

export type SubscriptionsSectionId =
  (typeof SUBSCRIPTIONS_SECTIONS)[number]['id']

const subscriptionsRegistry = createSectionRegistry<
  SubscriptionsSectionId,
  Record<string, never>,
  []
>({
  sections: SUBSCRIPTIONS_SECTIONS,
  defaultSection: 'plans',
  basePath: '/subscriptions',
  urlStyle: 'path',
})

export const SUBSCRIPTIONS_SECTION_IDS = subscriptionsRegistry.sectionIds
export const SUBSCRIPTIONS_DEFAULT_SECTION =
  subscriptionsRegistry.defaultSection

/** Type guard for validating section IDs without casting. */
export function isSubscriptionsSectionId(
  value: string
): value is SubscriptionsSectionId {
  return (SUBSCRIPTIONS_SECTION_IDS as readonly string[]).includes(value)
}

export const getSubscriptionsSectionNavItems =
  subscriptionsRegistry.getSectionNavItems
