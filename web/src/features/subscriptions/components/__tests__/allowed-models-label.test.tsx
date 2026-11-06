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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import type { ComponentProps } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, describe, expect, test } from 'vitest'

import { AllowedModelsLabel } from '../allowed-models-label'

const i18n = createInstance()
await i18n.init({
  lng: 'en',
  resources: { en: { translation: {} } },
  initAsync: false,
})

async function waitForTooltipContent(): Promise<HTMLElement> {
  return (await screen.findByText(
    (_content, element) =>
      element?.getAttribute?.('data-slot') === 'tooltip-content'
  )) as HTMLElement
}

function renderLabel(props: ComponentProps<typeof AllowedModelsLabel>) {
  render(
    <I18nextProvider i18n={i18n}>
      <AllowedModelsLabel {...props} />
    </I18nextProvider>
  )
}

afterEach(() => {
  cleanup()
  localStorage.clear()
})

describe('subscription model scope label', () => {
  test('an unscoped plan states that every model may spend it', () => {
    renderLabel({})
    expect(screen.getByText('Allowed Models: All models')).toBeTruthy()
  })

  test('an unscoped plan opens no tooltip because there is nothing to list', async () => {
    const user = userEvent.setup()
    renderLabel({})
    await user.hover(screen.getByText('Allowed Models: All models'))
    expect(screen.queryByText('gpt-5.5')).toBeNull()
  })

  test('a scoped plan lists its models inline and in the tooltip', async () => {
    const user = userEvent.setup()
    renderLabel({ allowed: ['gpt-5.5', 'claude-*'] })

    const inline = screen.getByText('Allowed Models: gpt-5.5, claude-*')
    expect(inline).toBeTruthy()
    await user.hover(inline)

    // The full list must stay reachable even when the inline text truncates,
    // one entry per line, so a long whitelist can be read. The shared
    // TooltipContent renders with data-slot="tooltip-content"; querying that
    // keeps the test on the project's own component contract instead of an
    // affordance added only for the test.
    const content = await waitForTooltipContent()
    expect(within(content).getByText('gpt-5.5')).toBeTruthy()
    expect(within(content).getByText('claude-*')).toBeTruthy()
    // Each entry is its own element, not one comma-joined string.
    expect(content.children).toHaveLength(2)
  })

  test('a scoped plan can be opened from the keyboard', async () => {
    const user = userEvent.setup()
    renderLabel({ allowed: ['gpt-5.5', 'claude-*'] })

    const trigger = screen.getByText('Allowed Models: gpt-5.5, claude-*')
    // Tabbing to it must focus it; a list that truncates is unreadable if it can
    // only be reached with a mouse.
    await user.tab()
    expect(document.activeElement).toBe(trigger)

    expect(await screen.findByText('gpt-5.5')).toBeTruthy()
  })

  test('count mode names the size inline and keeps every model in the tooltip', async () => {
    const user = userEvent.setup()
    renderLabel({
      allowed: ['gpt-5.5', 'claude-*', 'gemini-2.0'],
      asCount: true,
    })

    expect(screen.getByText('Allowed Models: 3 models')).toBeTruthy()
    expect(
      screen.queryByText('Allowed Models: gpt-5.5, claude-*, gemini-2.0')
    ).toBeNull()

    await user.hover(screen.getByText('Allowed Models: 3 models'))
    expect(await screen.findByText('gemini-2.0')).toBeTruthy()
  })

  test('hideWhenUnscoped renders nothing rather than a redundant All models line', () => {
    const { container } = render(
      <I18nextProvider i18n={i18n}>
        <AllowedModelsLabel allowed={[]} hideWhenUnscoped />
      </I18nextProvider>
    )
    expect(container).toBeEmptyDOMElement()
    expect(screen.queryByText(/All models/)).toBeNull()
  })

  test('hideWhenUnscoped still renders a scoped plan', () => {
    renderLabel({ allowed: ['gpt-5.5'], hideWhenUnscoped: true })
    expect(screen.getByText('Allowed Models: gpt-5.5')).toBeTruthy()
  })
})
