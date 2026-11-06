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
import {
  getCoreRowModel,
  getFilteredRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { toast } from 'sonner'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { SubscriptionListItem } from '../../types'
import { SubscriptionsListBulkActions } from '../subscriptions-list-bulk-actions'
import { SubscriptionsProvider } from '../subscriptions-provider'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

function makeRow(id: number, status = 'active'): SubscriptionListItem {
  return {
    subscription: {
      id,
      user_id: id,
      plan_id: 7,
      status,
      start_time: 1700000000,
      end_time: 1900000000,
      amount_total: 1000,
      amount_used: 100,
    },
    user_id: id,
    username: `user-${id}`,
    plan_id: 7,
    plan_title: 'Plan',
  }
}

const apiClient = api as unknown as {
  post: (url: string, data?: unknown) => Promise<{ data: unknown }>
}
const originalPost = apiClient.post

/**
 * Installs a POST stub that records every bulk request and answers it. A
 * `deferred` response lets a test hold one mutation in flight while it checks
 * what the other actions do.
 */
function installPostStub(response: {
  success: boolean
  data: unknown
  message?: string
}) {
  const posts: Array<{ url: string; body: unknown }> = []
  apiClient.post = async (url, body) => {
    posts.push({ url, body })
    return { data: response }
  }
  return posts
}

function renderToolbar(
  rows: SubscriptionListItem[] = [makeRow(11), makeRow(12)]
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  })
  // The provider reads the payment-compliance options through this query key;
  // seeding it keeps the fixture off the network.
  queryClient.setQueryData(
    ['system-options'],
    { data: [] },
    { updatedAt: Date.now() + 60_000 }
  )

  function Harness() {
    const [rowSelection, setRowSelection] = useState<Record<string, boolean>>(
      () =>
        Object.fromEntries(
          rows.map((row) => [String(row.subscription.id), true])
        )
    )
    const table = useReactTable({
      data: rows,
      columns: [
        {
          id: 'id',
          accessorFn: (row: SubscriptionListItem) => row.subscription.id,
        },
      ],
      getCoreRowModel: getCoreRowModel(),
      getFilteredRowModel: getFilteredRowModel(),
      enableRowSelection: true,
      getRowId: (row) => String(row.subscription.id),
      state: { rowSelection },
      onRowSelectionChange: setRowSelection,
    })
    return <SubscriptionsListBulkActions table={table} />
  }

  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <SubscriptionsProvider>
          <Harness />
        </SubscriptionsProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
  return queryClient
}

afterEach(() => {
  apiClient.post = originalPost
  cleanup()
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('cross-user subscription bulk actions', () => {
  test('invalidate sends exactly the selected ids and reports the counts', async () => {
    const successSpy = vi.spyOn(toast, 'success')
    const posts = installPostStub({
      success: true,
      data: { requested: 2, succeeded: 2, failed: 0, items: [] },
    })
    const user = userEvent.setup()
    renderToolbar()

    await user.click(
      screen.getByRole('button', { name: 'Invalidate selected subscriptions' })
    )
    const confirm = await screen.findByRole('button', { name: 'Invalidate' })
    await user.click(confirm)

    await waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0].url).toBe(
      '/api/subscription/admin/subscriptions/invalidate'
    )
    expect(posts[0].body).toEqual({ ids: [11, 12] })
    await waitFor(() =>
      expect(successSpy).toHaveBeenCalledWith('Invalidated 2 subscriptions')
    )
  })

  test('a partial failure is reported as partial, never as a plain success', async () => {
    const errorSpy = vi.spyOn(toast, 'error')
    const successSpy = vi.spyOn(toast, 'success')
    installPostStub({
      success: true,
      data: {
        requested: 2,
        succeeded: 1,
        failed: 1,
        items: [{ id: 12, ok: false, message: 'record not found' }],
      },
    })
    const user = userEvent.setup()
    renderToolbar()

    await user.click(
      screen.getByRole('button', { name: 'Invalidate selected subscriptions' })
    )
    await user.click(await screen.findByRole('button', { name: 'Invalidate' }))

    await waitFor(() =>
      expect(errorSpy).toHaveBeenCalledWith('1 invalidated, 1 failed')
    )
    expect(successSpy).not.toHaveBeenCalled()
  })

  test('a refused batch shows the server reason instead of a success toast', async () => {
    const errorSpy = vi.spyOn(toast, 'error')
    const successSpy = vi.spyOn(toast, 'success')
    installPostStub({
      success: false,
      data: null,
      message: 'no subscription selected',
    })
    const user = userEvent.setup()
    renderToolbar()

    await user.click(
      screen.getByRole('button', { name: 'Delete selected subscriptions' })
    )
    await user.click(await screen.findByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(errorSpy).toHaveBeenCalled())
    expect(successSpy).not.toHaveBeenCalled()
  })

  test('reset forwards the advance-reset switch and defaults it to on', async () => {
    const posts = installPostStub({
      success: true,
      data: { requested: 2, succeeded: 2, failed: 0, items: [] },
    })
    const user = userEvent.setup()
    renderToolbar()

    await user.click(
      screen.getByRole('button', { name: 'Reset selected subscription quota' })
    )
    const advance = await screen.findByRole('switch', {
      name: 'Advance next reset time',
    })
    expect(advance).toHaveAttribute('aria-checked', 'true')
    await user.click(screen.getByRole('button', { name: 'Reset' }))

    await waitFor(() => expect(posts).toHaveLength(1))
    expect(posts[0].url).toBe('/api/subscription/admin/subscriptions/reset')
    expect(posts[0].body).toEqual({ ids: [11, 12], advance_reset_time: true })
  })

  test('an empty selection renders no toolbar, so no action can run on nothing', () => {
    renderToolbar([])
    expect(
      screen.queryByRole('button', {
        name: 'Invalidate selected subscriptions',
      })
    ).toBeNull()
    expect(
      screen.queryByRole('button', { name: 'Delete selected subscriptions' })
    ).toBeNull()
  })

  test('while a reset is in flight no second write can start on the same rows', async () => {
    // Hold the reset open by never resolving its POST. The confirm dialog is
    // modal, so the toolbar behind it is aria-hidden from the accessibility
    // tree; `hidden: true` reaches those buttons to assert their real disabled
    // state, which is what stops a second concurrent write against the rows the
    // reset is about to change.
    const successSpy = vi.spyOn(toast, 'success')
    // A ref object keeps the resolver reachable: TypeScript does not track
    // assignments made inside the Promise executor, so a plain `let` narrows to
    // null at the call site.
    const gateControl: { release?: () => void } = {}
    const gate = new Promise<void>((resolve) => {
      gateControl.release = resolve
    })
    apiClient.post = async () => {
      await gate
      return {
        data: {
          success: true,
          data: { requested: 2, succeeded: 2, failed: 0, items: [] },
        },
      }
    }
    const user = userEvent.setup()
    renderToolbar()

    await user.click(
      screen.getByRole('button', { name: 'Reset selected subscription quota' })
    )
    await user.click(await screen.findByRole('button', { name: 'Reset' }))

    // The dialog's own confirm button locks first: that is the double-submit
    // guard an operator sees.
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Resetting...' })
      ).toBeDisabled()
    )

    const inToolbar = (name: string) =>
      screen.getByRole('button', { name, hidden: true })
    await waitFor(() =>
      expect(inToolbar('Invalidate selected subscriptions')).toBeDisabled()
    )
    expect(inToolbar('Delete selected subscriptions')).toBeDisabled()
    expect(inToolbar('Reset selected subscription quota')).toBeDisabled()

    gateControl.release?.()
    // Settling proves two things at once: the mutation reports its own success
    // and the selection is cleared, which unmounts the toolbar so no stale
    // action can fire against rows already reset.
    await waitFor(() =>
      expect(successSpy).toHaveBeenCalledWith('Reset 2 subscriptions')
    )
    await waitFor(() =>
      expect(
        screen.queryByRole('button', {
          name: 'Invalidate selected subscriptions',
          hidden: true,
        })
      ).toBeNull()
    )
  })
})
