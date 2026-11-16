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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  EMPTY_RESET_CARD_GRANT_DRAFT,
  type ResetCardGrantDraft,
} from '../../lib/reset-card-grant'
import { ResetCardRecipientFields } from '../reset-card-recipient-fields'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

const apiClient = api as unknown as {
  get: (url: string) => Promise<{ data: unknown }>
}
const originalGet = apiClient.get

/**
 * Answers the two queries the fields make: user search (by keyword) and the
 * recipient-count preview (page size 1). Any other endpoint returns empty data
 * so the test never touches the network.
 */
function installGetStub(total: number) {
  const calls: string[] = []
  apiClient.get = async (url) => {
    calls.push(url)
    if (url.startsWith('/api/user/search')) {
      return {
        data: {
          success: true,
          data: { items: [], total, page: 1, page_size: 1 },
        },
      }
    }
    if (url.startsWith('/api/group')) {
      return { data: { success: true, data: ['default', 'pro'] } }
    }
    return { data: { success: true, data: [] } }
  }
  return calls
}

function renderFields(initial: ResetCardGrantDraft = EMPTY_RESET_CARD_GRANT_DRAFT) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  let latest = initial

  function Harness() {
    const [draft, setDraft] = useState<ResetCardGrantDraft>(initial)
    latest = draft
    return (
      <ResetCardRecipientFields
        draft={draft}
        onChange={(next) => {
          latest = next
          setDraft(next)
        }}
      />
    )
  }

  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <Harness />
      </I18nextProvider>
    </QueryClientProvider>
  )
  return () => latest
}

afterEach(() => {
  apiClient.get = originalGet
  cleanup()
  vi.restoreAllMocks()
})

describe('reset card recipient fields', () => {
  test('switching to filter mode replaces the user picker with conditions', async () => {
    installGetStub(0)
    const user = userEvent.setup()
    const readDraft = renderFields()

    // The default mode is the explicit user list.
    expect(
      screen.getByPlaceholderText('Search users by username or ID...')
    ).toBeInTheDocument()

    await user.click(screen.getByRole('radio', { name: 'Users matching a filter' }))

    expect(readDraft().recipientMode).toBe('filter')
    expect(
      screen.queryByPlaceholderText('Search users by username or ID...')
    ).toBeNull()
    expect(
      screen.getByPlaceholderText('Username, display name or user ID')
    ).toBeInTheDocument()
  })

  test('codes mode explains that the codes are unbound', async () => {
    installGetStub(0)
    const user = userEvent.setup()
    const readDraft = renderFields()

    await user.click(screen.getByRole('radio', { name: 'Redeem codes' }))

    expect(readDraft().recipientMode).toBe('codes')
    expect(
      screen.getByText(/Codes are not bound to anyone/)
    ).toBeInTheDocument()
  })

  test('leaving the user mode drops a subscription target the server would refuse', async () => {
    installGetStub(0)
    const user = userEvent.setup()
    const readDraft = renderFields({
      ...EMPTY_RESET_CARD_GRANT_DRAFT,
      recipientMode: 'users',
      targetScope: 'subscription',
      userSubscriptionId: '9',
    })

    await user.click(screen.getByRole('radio', { name: 'All users' }))

    const next = readDraft()
    expect(next.recipientMode).toBe('all')
    expect(next.targetScope).toBe('all')
  })

  test('the preview reports how many users the filter matches', async () => {
    installGetStub(42)
    const user = userEvent.setup()
    renderFields()

    await user.click(screen.getByRole('radio', { name: 'All users' }))

    await waitFor(() =>
      expect(screen.getByText('42 user(s) will receive cards')).toBeInTheDocument()
    )
  })

  test('over the batch limit the preview turns into an error', async () => {
    installGetStub(501)
    const user = userEvent.setup()
    renderFields()

    await user.click(screen.getByRole('radio', { name: 'All users' }))

    await waitFor(() =>
      expect(
        screen.getByText(
          '501 users match, over the 500 per batch. Narrow the filter.'
        )
      ).toBeInTheDocument()
    )
  })
})
