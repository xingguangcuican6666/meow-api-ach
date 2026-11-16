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

import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useMemo, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

/**
 * react-hook-form reads dotted names as nested paths, so the form is modelled
 * as a nested object and flattened back to `harness_usage_setting.*` on save.
 */
const harnessUsageSchema = z.object({
  harness_usage_setting: z.object({
    enabled: z.boolean(),
    codex_limit_id: z.string(),
  }),
})

type HarnessUsageFormInput = z.input<typeof harnessUsageSchema>
type HarnessUsageFormValues = z.output<typeof harnessUsageSchema>

type FlatHarnessUsageDefaults = {
  'harness_usage_setting.enabled': boolean
  'harness_usage_setting.codex_limit_id': string
}

const buildFormDefaults = (
  defaults: FlatHarnessUsageDefaults
): HarnessUsageFormInput => ({
  harness_usage_setting: {
    enabled: defaults['harness_usage_setting.enabled'],
    codex_limit_id: defaults['harness_usage_setting.codex_limit_id'] ?? '',
  },
})

const normalizeFormValues = (
  values: HarnessUsageFormValues
): FlatHarnessUsageDefaults => ({
  'harness_usage_setting.enabled': values.harness_usage_setting.enabled,
  'harness_usage_setting.codex_limit_id':
    values.harness_usage_setting.codex_limit_id ?? '',
})

type HarnessUsageSectionProps = {
  defaultValues: FlatHarnessUsageDefaults
}

export function HarnessUsageSection({
  defaultValues,
}: HarnessUsageSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<HarnessUsageFormInput, unknown, HarnessUsageFormValues>({
    resolver: zodResolver(harnessUsageSchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatHarnessUsageDefaults>(defaultValues)
  const baselineSerializedRef = useRef<string>(JSON.stringify(defaultValues))

  useEffect(() => {
    const serialized = JSON.stringify(defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(defaultValues))
  }, [defaultValues, form])

  const onSubmit = async (values: HarnessUsageFormValues) => {
    const normalized = normalizeFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof FlatHarnessUsageDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      await updateOption.mutateAsync({ key, value: normalized[key] })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset(buildFormDefaults(normalized))
  }

  return (
    <SettingsSection title={t('Harness Usage Headers')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <Alert>
            <AlertDescription>
              {t(
                'Writes subscription quota usage into the response headers Claude Code and Codex CLI read. Only plans with the per-plan switch enabled are reported, and nothing is sent for users without such a subscription.'
              )}
            </AlertDescription>
          </Alert>

          <FormField
            control={form.control}
            name='harness_usage_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Harness Usage Headers')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Master switch. When off, no quota headers are written and upstream quota headers are still stripped'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='harness_usage_setting.codex_limit_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Codex limit id')}</FormLabel>
                <FormControl>
                  <Input className='max-w-xs' placeholder='codex' {...field} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Header family name used for Codex, e.g. codex sends x-codex-primary-used-percent. Leave empty for the default'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
