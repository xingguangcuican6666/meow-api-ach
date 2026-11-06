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
import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import { formatAllowedModels } from '../lib'

interface AllowedModelsLabelProps {
  /** The plan's model whitelist; absent or empty means every model. */
  allowed?: readonly string[] | null
  /**
   * Render nothing when the plan covers every model. Lists of subscriptions use
   * this so unscoped plans do not each carry a redundant line.
   */
  hideWhenUnscoped?: boolean
  /**
   * Show a model count instead of the inline list, with the full list in the
   * tooltip. Used where horizontal space is tight.
   */
  asCount?: boolean
  className?: string
}

/**
 * States which models a subscription plan's quota may be spent on.
 *
 * The scope is a billing fact, not a detail: a model outside the whitelist never
 * draws down the plan, and on subscription-only billing the request is rejected.
 * So every surface that shows a plan or a subscription shows this, with the full
 * list always reachable in the tooltip because a whitelist can hold hundreds of
 * entries.
 */
export function AllowedModelsLabel(props: AllowedModelsLabelProps) {
  const { t } = useTranslation()
  const allowed = props.allowed ?? []
  const scoped = allowed.length > 0

  if (!scoped && props.hideWhenUnscoped) return null

  const summary =
    props.asCount && scoped
      ? t('{{count}} models', { count: allowed.length })
      : formatAllowedModels(allowed, t)

  const label = (
    <span
      className={cn(
        'text-muted-foreground/80 block truncate',
        // The inline text truncates, so hint that hovering expands it whenever
        // there is a tooltip to expand — in both list and count mode.
        scoped ? 'cursor-help' : undefined,
        props.className
      )}
      // A truncated whitelist is otherwise unreachable by keyboard, so the
      // trigger takes focus and the tooltip opens on focus as well as hover.
      tabIndex={scoped ? 0 : undefined}
    >
      {t('Allowed Models')}: {summary}
    </span>
  )

  if (!scoped) {
    // Nothing to enumerate, so no tooltip: an empty popover would be worse than
    // the plain "All models" line.
    return label
  }

  return (
    <Tooltip>
      <TooltipTrigger render={label} />
      <TooltipContent>
        <div className='flex max-h-60 flex-col gap-0.5 overflow-y-auto'>
          {allowed.map((model) => (
            <span key={model}>{model}</span>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  )
}
