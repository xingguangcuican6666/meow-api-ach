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
import { createFileRoute, redirect } from '@tanstack/react-router'
import z from 'zod'

import { Subscriptions } from '@/features/subscriptions'
import { SUBSCRIPTIONS_SECTION_IDS } from '@/features/subscriptions/section-registry'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

// The cross-user subscription list keeps its table state in the URL under `s*`
// keys so a tab switch does not clobber the plan catalog's own view state.
const subscriptionsSearchSchema = z.object({
  sPage: z.number().optional().catch(1),
  sPageSize: z.number().optional().catch(20),
  sFilter: z.string().optional().catch(''),
  sStatus: z.array(z.string()).optional().catch([]),
})

export const Route = createFileRoute('/_authenticated/subscriptions/$section')({
  beforeLoad: ({ params }) => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }

    const validSections = SUBSCRIPTIONS_SECTION_IDS as unknown as string[]
    if (!validSections.includes(params.section)) {
      throw redirect({
        to: '/subscriptions/$section',
        params: { section: 'plans' },
      })
    }
  },
  validateSearch: subscriptionsSearchSchema,
  component: Subscriptions,
})
