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
import type { ResetCardGrantDraft } from '../lib/reset-card-grant'
import { ResetCardConfigFields } from './reset-card-config-fields'
import { ResetCardRecipientFields } from './reset-card-recipient-fields'

interface Props {
  draft: ResetCardGrantDraft
  onChange: (draft: ResetCardGrantDraft) => void
  disabled?: boolean
}

/**
 * The full grant form: who gets the cards, then what a card does. The two
 * halves are separate components because the users-page bulk action only needs
 * the second one — its recipients are already the selected table rows.
 */
export function ResetCardGrantFields(props: Props) {
  return (
    <div className='flex flex-col gap-5'>
      <ResetCardRecipientFields
        draft={props.draft}
        onChange={props.onChange}
        disabled={props.disabled}
      />
      <ResetCardConfigFields
        draft={props.draft}
        onChange={props.onChange}
        disabled={props.disabled}
        // An ownerless code cannot match a subscription that belongs to one
        // person, so the server refuses that combination.
        blockedScopes={
          props.draft.recipientMode === 'codes' ? ['subscription'] : undefined
        }
      />
    </div>
  )
}
