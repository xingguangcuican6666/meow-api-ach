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
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { Info, Plus } from 'lucide-react'
import { useCallback, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { GrantResetCardDialog } from './components/dialogs/grant-reset-card-dialog'
import { ResetCardsTable } from './components/reset-cards-table'
import { SubscriptionsDialogs } from './components/subscriptions-dialogs'
import { SubscriptionsListTable } from './components/subscriptions-list-table'
import { SubscriptionsPrimaryButtons } from './components/subscriptions-primary-buttons'
import {
  SubscriptionsProvider,
  useSubscriptions,
} from './components/subscriptions-provider'
import { SubscriptionsTable } from './components/subscriptions-table'
import {
  isSubscriptionsSectionId,
  SUBSCRIPTIONS_DEFAULT_SECTION,
  type SubscriptionsSectionId,
} from './section-registry'

const route = getRouteApi('/_authenticated/subscriptions/$section')

const SECTION_META: Record<
  SubscriptionsSectionId,
  { titleKey: string; tabKey: string }
> = {
  plans: {
    titleKey: 'Subscription Management',
    tabKey: 'Subscription Plans',
  },
  subscriptions: {
    titleKey: 'User Subscriptions',
    tabKey: 'User Subscriptions',
  },
  'reset-cards': {
    titleKey: 'Reset Cards',
    tabKey: 'Reset Cards',
  },
}

const SECTION_IDS = ['plans', 'subscriptions', 'reset-cards'] as const

function SubscriptionsContent() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const params = route.useParams()
  const activeSection: SubscriptionsSectionId =
    params.section && isSubscriptionsSectionId(params.section)
      ? params.section
      : SUBSCRIPTIONS_DEFAULT_SECTION
  const { complianceConfirmed } = useSubscriptions()
  const isRoot = useAuthStore(
    (state) => state.auth.user?.role === ROLE.SUPER_ADMIN
  )
  const [grantOpen, setGrantOpen] = useState(false)

  const handleSectionChange = useCallback(
    (section: string) => {
      void navigate({
        to: '/subscriptions/$section',
        params: { section: section as SubscriptionsSectionId },
        search: (previous) => previous,
      })
    },
    [navigate]
  )

  const meta = SECTION_META[activeSection]
  // The plan catalog owns the create-plan alert and button; the cross-user list
  // has no primary action of its own, since bulk actions live in the selection
  // toolbar that appears once rows are selected. The card ledger's only primary
  // action is minting new cards, which is root-only.
  let actions: ReactNode = null
  if (activeSection === 'plans') {
    actions = <SubscriptionsPrimaryButtons />
  } else if (activeSection === 'reset-cards' && isRoot) {
    actions = (
      <Button size='sm' onClick={() => setGrantOpen(true)}>
        <Plus className='h-4 w-4' />
        {t('Grant reset cards')}
      </Button>
    )
  }

  let sectionContent: ReactNode = <ResetCardsTable />
  if (activeSection === 'plans') {
    sectionContent = <SubscriptionsTable />
  } else if (activeSection === 'subscriptions') {
    sectionContent = <SubscriptionsListTable />
  }

  return (
    <>
      <SectionPageLayout fixedContent>
        <SectionPageLayout.Title>{t(meta.titleKey)}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <div className='flex items-center gap-2'>
            {activeSection === 'plans' ? (
              <Alert variant='default' className='hidden px-3 py-2 sm:flex'>
                <Info className='h-4 w-4' />
                <AlertDescription className='text-xs'>
                  {t(
                    'Stripe/Creem requires creating products on the third-party platform and entering the ID'
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
            {actions}
          </div>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex h-full min-h-0 flex-col gap-4'>
            <Tabs value={activeSection} onValueChange={handleSectionChange}>
              <TabsList className='max-w-full flex-wrap justify-start group-data-horizontal/tabs:h-auto'>
                {SECTION_IDS.map((section) => (
                  <TabsTrigger key={section} value={section}>
                    {t(SECTION_META[section].tabKey)}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
            {!complianceConfirmed && activeSection === 'plans' ? (
              <Alert variant='destructive' className='shrink-0'>
                <AlertDescription>
                  {t(
                    'Subscription plan creation and changes are locked until the administrator confirms compliance terms in Payment Gateway settings.'
                  )}
                </AlertDescription>
              </Alert>
            ) : null}
            <div className='min-h-0 flex-1'>{sectionContent}</div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <SubscriptionsDialogs />
      <GrantResetCardDialog open={grantOpen} onOpenChange={setGrantOpen} />
    </>
  )
}

export function Subscriptions() {
  return (
    <SubscriptionsProvider>
      <SubscriptionsContent />
    </SubscriptionsProvider>
  )
}
