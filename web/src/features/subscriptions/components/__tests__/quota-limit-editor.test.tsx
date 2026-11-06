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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test } from 'vitest'

const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { api } = await import('@/lib/api')
const { SubscriptionsProvider } = await import('../subscriptions-provider')
const { SubscriptionsMutateDrawer } =
  await import('../subscriptions-mutate-drawer')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'en',
  resources: { en: { translation: {} } },
})

type ApiMethod = (url: string, data?: unknown) => Promise<{ data: unknown }>
type MockableApi = { get: ApiMethod; post: ApiMethod; put: ApiMethod }

const apiClient = api as unknown as MockableApi
const originalGet = apiClient.get
const originalPost = apiClient.post
const originalPut = apiClient.put
let activeClient: InstanceType<typeof QueryClient> | null = null

function installApiFixtures(createdPayloads: Array<Record<string, unknown>>) {
  apiClient.get = async (url) => {
    switch (url) {
      case '/api/group':
        return { data: { success: true, data: ['default', 'vip'] } }
      case '/api/models/':
        return {
          data: {
            success: true,
            data: {
              items: [
                { id: 1, model_name: 'gpt-4o' },
                { id: 2, model_name: 'claude-sonnet-4' },
              ],
              total: 2,
              page: 1,
              page_size: 500,
            },
          },
        }
      case '/api/option/':
        return { data: { success: true, data: [] } }
      case '/api/option/waffo-pancake/subscription-product-options':
        return { data: { message: 'success', data: { products: [] } } }
      default:
        throw new Error(`Unexpected GET ${url}`)
    }
  }
  apiClient.post = async (url, data) => {
    expect(url).toBe('/api/subscription/admin/plans')
    createdPayloads.push(data as Record<string, unknown>)
    return { data: { success: true, data: {} } }
  }
  apiClient.put = async () => {
    throw new Error('unexpected PUT')
  }
}

function renderCreateDrawer() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClient.setQueryData(
    ['system-options'],
    { data: [] },
    { updatedAt: Date.now() + 60_000 }
  )
  activeClient = queryClient
  render(
    <QueryClientProvider client={queryClient}>
      <I18nextProvider i18n={i18n}>
        <SubscriptionsProvider>
          <SubscriptionsMutateDrawer open onOpenChange={() => undefined} />
        </SubscriptionsProvider>
      </I18nextProvider>
    </QueryClientProvider>
  )
}

afterEach(() => {
  apiClient.get = originalGet
  apiClient.post = originalPost
  apiClient.put = originalPut
  if (activeClient) {
    activeClient.clear()
    activeClient = null
  }
  localStorage.clear()
})

describe('subscription plan extra quota limit editor', () => {
  test('a plan form starts with no limit rows and states that plainly', async () => {
    installApiFixtures([])
    renderCreateDrawer()

    expect(await screen.findByText('Extra Quota Limits')).toBeTruthy()
    expect(screen.getByText('No extra limit configured.')).toBeTruthy()
    expect(screen.queryByText('Quota Limit #1')).toBeNull()
  })

  test('adding rows allows a 5h limit and a 7d limit side by side', async () => {
    installApiFixtures([])
    renderCreateDrawer()

    const addButton = await screen.findByRole('button', {
      name: /Add limit/,
    })
    fireEvent.click(addButton)
    fireEvent.click(addButton)

    expect(await screen.findByText('Quota Limit #1')).toBeTruthy()
    expect(screen.getByText('Quota Limit #2')).toBeTruthy()
    expect(screen.queryByText('No extra limit configured.')).toBeNull()

    // Each row owns its own window selector and per-model picker, so a 5h and
    // a 7d limit can be configured at the same time.
    expect(screen.getAllByText('Window')).toHaveLength(2)
    expect(screen.getAllByText('Models (empty for all models)')).toHaveLength(2)
  })

  test('a row can be removed again without touching the others', async () => {
    installApiFixtures([])
    renderCreateDrawer()

    const addButton = await screen.findByRole('button', { name: /Add limit/ })
    fireEvent.click(addButton)
    fireEvent.click(addButton)
    await screen.findByText('Quota Limit #2')

    const removeButtons = screen.getAllByRole('button', {
      name: 'Remove limit',
    })
    fireEvent.click(removeButtons[0])

    await waitFor(() => {
      expect(screen.getAllByText(/Quota Limit #/)).toHaveLength(1)
    })
    expect(screen.getByText('Quota Limit #1')).toBeTruthy()
    expect(screen.queryByText('Quota Limit #2')).toBeNull()
  })

  test('a configured 5h limit is sent with the plan payload', async () => {
    const created: Array<Record<string, unknown>> = []
    installApiFixtures(created)
    renderCreateDrawer()

    fireEvent.change(await screen.findByPlaceholderText('e.g. Basic Plan'), {
      target: { value: 'Pro' },
    })
    fireEvent.click(await screen.findByRole('button', { name: /Add limit/ }))
    await screen.findByText('Quota Limit #1')

    const limitNames = screen.getAllByPlaceholderText('e.g. 5h limit')
    fireEvent.change(limitNames[0], { target: { value: '5h' } })

    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => expect(created).toHaveLength(1))
    const plan = created[0].plan as Record<string, unknown>
    const limits = plan.quota_limits as Array<Record<string, unknown>>
    expect(limits).toHaveLength(1)
    expect(limits[0].name).toBe('5h')
    expect(limits[0].key).toBe('limit-1')
    expect(limits[0].window_seconds).toBe(0)
    expect(limits[0].models).toEqual([])
  })

  test('a 5h window and a per-model scope are sent for the same plan', async () => {
    const user = userEvent.setup()
    const created: Array<Record<string, unknown>> = []
    installApiFixtures(created)
    renderCreateDrawer()

    fireEvent.change(await screen.findByPlaceholderText('e.g. Basic Plan'), {
      target: { value: 'Pro' },
    })

    const addButton = await screen.findByRole('button', { name: /Add limit/ })
    fireEvent.click(addButton)
    fireEvent.click(addButton)
    await screen.findByText('Quota Limit #2')

    // Row 1: a 5 hour rolling window over every model.
    const [firstWindow] = screen.getAllByLabelText('Window')
    await user.click(firstWindow)
    await user.click(await screen.findByRole('option', { name: '5 hours' }))

    // Row 2: a per-model limit scoped to one configured model. Opening the
    // picker lists the configured models; picking one scopes the limit to it.
    const [firstModels, secondModels] = screen.getAllByLabelText(
      'Models (empty for all models)'
    )
    expect(firstModels).toBeTruthy()
    await user.click(secondModels)
    fireEvent.change(secondModels, { target: { value: 'claude' } })
    const option = await screen.findByRole('option', {
      name: /claude-sonnet-4/,
    })
    await user.click(option)

    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }))

    await waitFor(() => expect(created).toHaveLength(1))
    const plan = created[0].plan as Record<string, unknown>
    const limits = plan.quota_limits as Array<Record<string, unknown>>
    expect(limits).toHaveLength(2)
    expect(limits[0].window_seconds).toBe(5 * 60 * 60)
    expect(limits[0].models).toEqual([])
    expect(limits[1].models).toEqual(['claude-sonnet-4'])
  })
})
