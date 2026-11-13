/*
Copyright (C) 2023-2026 xingguangcuican6666

This file is part of new-api, which is licensed under the GNU Affero General
Public License v3.0. Original project: https://github.com/QuantumNous/new-api

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
*/
import { describe, expect, it } from 'vitest'

import {
  getAdvancedCustomConverterDefaults,
  getAdvancedCustomConverterOptions,
  isAdvancedCustomIncomingPathAllowed,
} from '../advanced-custom'

describe('pure Anthropic Messages to OpenAI Chat converter', () => {
  it('is offered for the Claude Messages incoming path', () => {
    const values = getAdvancedCustomConverterOptions('/v1/messages').map(
      (option) => option.value
    )

    expect(values).toContain('anthropic_messages_to_openai_chat_completions')
    expect(values).toContain(
      'anthropic_messages_to_openai_chat_completions_pure'
    )
  })

  it('rejects incoming paths other than Claude Messages', () => {
    expect(
      isAdvancedCustomIncomingPathAllowed(
        '/v1/messages',
        'anthropic_messages_to_openai_chat_completions_pure'
      )
    ).toBe(true)
    expect(
      isAdvancedCustomIncomingPathAllowed(
        '/v1/chat/completions',
        'anthropic_messages_to_openai_chat_completions_pure'
      )
    ).toBe(false)
  })

  it('defaults to an OpenAI chat upstream with bearer auth', () => {
    const defaults = getAdvancedCustomConverterDefaults(
      'anthropic_messages_to_openai_chat_completions_pure',
      '/v1/messages'
    )

    expect(defaults.upstream_path).toBe('/v1/chat/completions')
    expect(defaults.auth).toEqual({
      type: 'header',
      name: 'Authorization',
      value: 'Bearer {api_key}',
    })
  })
})
