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

package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// Claude Code and Codex CLI both read their quota bars from response headers:
// Claude Code parses the anthropic-ratelimit-unified-* family for /status, and
// Codex parses the x-<limit>-primary-* family plus x-codex-credits-*. Neither
// client can see subscription usage that only exists in new-api, so this file
// projects the opted-in subscriptions onto those headers.
//
// Two properties drive the design:
//   - Numbers are a pre-request snapshot. Settlement happens after the response
//     body is written, so post-settle values can never reach the headers.
//   - Nothing is emitted for a user without an opted-in active subscription.
//     Both clients treat a missing header family as "no cap", which is the
//     truthful answer here.

const (
	claudeHeaderPrefix = "anthropic-ratelimit-unified-"
	// claudeUpstreamPrefix is broader than claudeHeaderPrefix: every
	// anthropic-ratelimit-* header describes the upstream account, so all of
	// them are stripped, not only the unified family we emit ourselves.
	claudeUpstreamPrefix = "anthropic-ratelimit-"
	claudeClaim5h        = "5h"
	claudeClaim7d        = "7d"
)

// harnessUsageWindow is one merged quota window after combining every opted-in
// subscription. Group is empty for windows that cover every model and names the
// model family for per-model limits.
type harnessUsageWindow struct {
	WindowSeconds int64
	Group         string
	Total         int64
	Used          int64
	// WindowFrom is the latest anchor among the merged limits, or 0 when none of
	// them has a running window. Callers turn it into a countdown through
	// WindowResetAt.
	WindowFrom int64
}

// utilization returns the used fraction, clamped to [0, 1] so a stale counter
// that somehow exceeded its cap cannot render as a bar past full.
func (w harnessUsageWindow) utilization() float64 {
	if w.Total <= 0 {
		return 0
	}
	used := float64(w.Used) / float64(w.Total)
	return min(max(used, 0), 1)
}

// WindowResetAt returns when the rolling window anchored at windowFrom rolls, or
// 0 when there is no such moment to report.
//
// The anchor only moves when a window actually restarts, so anchor + window is a
// real countdown that decreases on every refresh while the window is open. Two
// states have no answer, and reporting a made-up one is what once pinned the bar
// at a full window forever:
//   - windowFrom <= 0: nothing has charged the counter (or an admin reset cleared
//     it), so no window is running. now + window would be a fresh full window on
//     every single read.
//   - anchor + window already elapsed: windows roll lazily on the next charge, so
//     the old anchor is history and the real restart is whenever traffic returns.
func WindowResetAt(windowFrom int64, windowSeconds int64, now int64) int64 {
	if windowFrom <= 0 || windowSeconds <= 0 {
		return 0
	}
	resetAt := windowFrom + windowSeconds
	if resetAt <= now {
		return 0
	}
	return resetAt
}

// SnapshotHarnessUsageHeaders computes the quota headers once, after billing has
// reserved the request and before it is sent upstream, and stores them on the
// gin context. WriteHarnessUsageHeaders later writes them verbatim.
func SnapshotHarnessUsageHeaders(c *gin.Context, info *relaycommon.RelayInfo) {
	if c == nil || info == nil || info.UserId <= 0 {
		return
	}
	setting := operation_setting.GetHarnessUsageSetting()
	if setting == nil || !setting.Enabled {
		return
	}

	windows := collectHarnessUsageWindows(info.UserId)
	if len(windows) == 0 {
		// No opted-in subscription: emit nothing at all, which is what both
		// harnesses interpret as "this account has no cap to display".
		return
	}

	now := common.GetTimestamp()
	headers := make(map[string]string)
	switch harnessHeaderFamily(info) {
	case types.RelayFormatClaude:
		fillClaudeUsageHeaders(headers, windows, walletUsage{
			Available: walletAvailable(info),
			InUse:     info.BillingSource == BillingSourceWallet,
		}, now)
	case types.RelayFormatOpenAIResponses:
		balance := 0
		if info != nil {
			balance = info.UserQuota
		}
		fillCodexUsageHeaders(headers, windows, setting.CodexHeaderPrefix(), balance, now)
	}
	if len(headers) == 0 {
		return
	}
	common.SetContextKey(c, constant.ContextKeyHarnessUsageHeaders, headers)
}

// WriteHarnessUsageHeaders copies the snapshot onto the downstream response.
// It is a no-op when no snapshot exists or headers were already sent.
func WriteHarnessUsageHeaders(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	value, ok := common.GetContextKeyType[map[string]string](c, constant.ContextKeyHarnessUsageHeaders)
	if !ok || len(value) == 0 {
		return
	}
	for k, v := range value {
		c.Writer.Header().Set(k, v)
	}
}

// IsUpstreamUsageLimitHeader reports whether an upstream response header belongs
// to a quota family this gateway reports itself. Copying the upstream's own
// numbers would show the client caps that do not apply to this account, so they
// are stripped and our snapshot wins.
//
// The x-codex-* match is deliberately narrow: X-Codex-Turn-State is a session
// continuation header, and dropping it would break multi-turn Codex requests.
func IsUpstreamUsageLimitHeader(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(lower, claudeUpstreamPrefix) {
		return true
	}
	rest, ok := strings.CutPrefix(lower, "x-codex-")
	if !ok {
		return false
	}
	switch {
	case strings.HasPrefix(rest, "credits-"):
		return true
	case rest == "rate-limit-reached-type", rest == "limit-name":
		return true
	}
	for _, suffix := range []string{
		"-used-percent",
		"-window-minutes",
		"-reset-at",
		"-limit-name",
	} {
		if strings.HasSuffix(rest, suffix) {
			return true
		}
	}
	return false
}

// harnessHeaderFamily picks which header dialect to speak from the request
// itself, mirroring how channel affinity matches model and path.
func harnessHeaderFamily(info *relaycommon.RelayInfo) types.RelayFormat {
	if info == nil {
		return ""
	}
	path := strings.ToLower(info.RequestURLPath)
	modelName := strings.ToLower(strings.TrimSpace(info.OriginModelName))
	if info.RelayFormat == types.RelayFormatClaude ||
		strings.Contains(path, "/v1/messages") ||
		strings.HasPrefix(modelName, "claude-") {
		return types.RelayFormatClaude
	}
	if info.RelayFormat == types.RelayFormatOpenAIResponses ||
		strings.Contains(path, "/v1/responses") ||
		strings.HasPrefix(modelName, "gpt-") ||
		strings.HasPrefix(modelName, "codex") ||
		isOpenAIOReasoningModel(modelName) {
		return types.RelayFormatOpenAIResponses
	}
	return ""
}

// isOpenAIOReasoningModel matches the o1/o3/o4 family without accepting every
// model whose name merely starts with "o".
func isOpenAIOReasoningModel(modelName string) bool {
	if len(modelName) < 2 || modelName[0] != 'o' {
		return false
	}
	return modelName[1] >= '0' && modelName[1] <= '9'
}

// collectHarnessUsageWindows merges the quota windows of every active
// subscription whose plan opted in. Limits sharing a (window, group) pair are
// summed, so two active subscriptions both capping 5h contribute one bar with
// the combined allowance.
func collectHarnessUsageWindows(userId int) []harnessUsageWindow {
	subs, err := model.GetAllActiveUserSubscriptions(userId)
	if err != nil {
		return nil
	}
	now := common.GetTimestamp()
	merged := make(map[string]*harnessUsageWindow)
	for _, summary := range subs {
		if summary.Subscription == nil {
			continue
		}
		plan, err := model.GetSubscriptionPlanById(summary.Subscription.PlanId)
		if err != nil || !plan.ReportsUsageToHarness() {
			continue
		}
		for _, limit := range plan.GetQuotaLimits() {
			if limit.WindowSeconds <= 0 {
				continue
			}
			groups := []string{""}
			if len(limit.Models) > 0 {
				// Two model patterns can land on the same group ("claude-sonnet-*"
				// and "claude-sonnet-4-5" are both "sonnet"). Keep one group per
				// family so the cap is not counted twice.
				groups = groups[:0]
				seen := make(map[string]struct{}, len(limit.Models))
				for _, pattern := range limit.Models {
					group := harnessModelGroup(pattern, limit.Key)
					if _, ok := seen[group]; ok {
						continue
					}
					seen[group] = struct{}{}
					groups = append(groups, group)
				}
			}
			for _, group := range groups {
				used, windowFrom := harnessLimitStateUsage(summary.Subscription, limit, group, now)
				key := fmt.Sprintf("%d|%s", limit.WindowSeconds, group)
				window, ok := merged[key]
				if !ok {
					window = &harnessUsageWindow{WindowSeconds: limit.WindowSeconds, Group: group}
					merged[key] = window
				}
				window.Total += limit.AmountTotal
				window.Used += used
				if windowFrom > window.WindowFrom {
					window.WindowFrom = windowFrom
				}
			}
		}
	}
	windows := make([]harnessUsageWindow, 0, len(merged))
	for _, window := range merged {
		windows = append(windows, *window)
	}
	return windows
}

// harnessLimitStateUsage sums the counters of one limit that belong to group,
// the way the UI does: the window is rolled in memory only, so reporting never
// writes back to the database.
//
// A model-scoped limit keeps one counter per model name, so a family can own
// several counters ("claude-sonnet-4-5" and "claude-sonnet-4-6" are both
// "sonnet"); they add up into the one bar for that family.
func harnessLimitStateUsage(sub *model.UserSubscription, limit model.SubscriptionQuotaLimit, group string, now int64) (int64, int64) {
	if sub == nil {
		return 0, 0
	}
	var used, windowFrom int64
	matched := false
	for _, state := range sub.QuotaLimitStates {
		if state.Key != limit.Key {
			continue
		}
		if len(limit.Models) > 0 && state.Model != "" && harnessModelGroup(state.Model, limit.Key) != group {
			continue
		}
		if len(limit.Models) == 0 && state.Model != "" {
			continue
		}
		matched = true
		counterUsed := state.Used
		counterFrom := state.WindowFrom
		if counterFrom > 0 && now >= counterFrom+limit.WindowSeconds {
			// The window elapsed but nothing has charged the counter since, so it
			// has not restarted yet: the old anchor is history and there is no new
			// one to count down to.
			counterUsed = 0
			counterFrom = 0
		}
		used += counterUsed
		if counterFrom > windowFrom {
			// Report the last of the family's windows to roll: until then the
			// group is not fully free again.
			windowFrom = counterFrom
		}
	}
	if !matched {
		return 0, 0
	}
	return used, windowFrom
}

// harnessModelGroup derives the short group name both harnesses use for
// per-model bars ("sonnet", "opus", "fable"). It takes the last all-letter
// segment of the model pattern, so claude-sonnet-4-5 becomes sonnet and
// gpt-5.2-codex becomes codex. When nothing qualifies it falls back to the
// limit key, which is stable and unique.
func harnessModelGroup(pattern string, fallbackKey string) string {
	trimmed := strings.Trim(strings.TrimSpace(pattern), "*")
	segments := strings.Split(strings.ToLower(trimmed), "-")
	for i := len(segments) - 1; i >= 0; i-- {
		segment := segments[i]
		if segment == "" {
			continue
		}
		if isASCIIAlpha(segment) {
			return segment
		}
	}
	key := strings.TrimSpace(fallbackKey)
	if key == "" {
		return "default"
	}
	return sanitizeHeaderToken(key)
}

func isASCIIAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return len(s) > 0
}

// sanitizeHeaderToken keeps a value usable inside a header name.
func sanitizeHeaderToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "default"
	}
	return out
}

// walletUsage is the overage state the Claude family reports: whether the
// wallet can still pay, and whether this request is already spending it.
type walletUsage struct {
	Available bool
	InUse     bool
}

// fillClaudeUsageHeaders writes the anthropic-ratelimit-unified-* family.
//
// Only the 5h and 7d windows have claim abbreviations Claude Code understands,
// so any other window length is simply not reported. Utilization is a 0-1
// fraction with two decimals; reset is unix seconds. surpassed-threshold and
// fallback are intentionally omitted: sending them would take over Claude
// Code's early-warning logic.
func fillClaudeUsageHeaders(headers map[string]string, windows []harnessUsageWindow, wallet walletUsage, now int64) {
	has5h, has7d := false, false
	var reset5h, reset7d int64
	for _, window := range windows {
		if window.Group != "" {
			continue
		}
		var abbrev string
		switch window.WindowSeconds {
		case model.SubscriptionLimitWindow5h:
			abbrev, has5h = claudeClaim5h, true
			if reset := WindowResetAt(window.WindowFrom, window.WindowSeconds, now); reset > reset5h {
				reset5h = reset
			}
		case model.SubscriptionLimitWindow7d:
			abbrev, has7d = claudeClaim7d, true
			if reset := WindowResetAt(window.WindowFrom, window.WindowSeconds, now); reset > reset7d {
				reset7d = reset
			}
		default:
			continue
		}
		headers[claudeHeaderPrefix+abbrev+"-utilization"] = fmt.Sprintf("%.2f", window.utilization())
		if reset := WindowResetAt(window.WindowFrom, window.WindowSeconds, now); reset > 0 {
			headers[claudeHeaderPrefix+abbrev+"-reset"] = fmt.Sprintf("%d", reset)
		}
	}
	for _, window := range windows {
		if window.Group == "" {
			continue
		}
		var abbrev string
		switch window.WindowSeconds {
		case model.SubscriptionLimitWindow5h:
			abbrev = claudeClaim5h
		case model.SubscriptionLimitWindow7d:
			abbrev = claudeClaim7d
		default:
			continue
		}
		prefix := claudeHeaderPrefix + abbrev + "_" + sanitizeHeaderToken(window.Group)
		headers[prefix+"-utilization"] = fmt.Sprintf("%.2f", window.utilization())
		if reset := WindowResetAt(window.WindowFrom, window.WindowSeconds, now); reset > 0 {
			headers[prefix+"-reset"] = fmt.Sprintf("%d", reset)
		}
	}

	if !has5h && !has7d {
		return
	}
	headers[claudeHeaderPrefix+"status"] = "allowed"
	switch {
	case has7d:
		headers[claudeHeaderPrefix+"representative-claim"] = claudeClaim7d
		if reset7d > 0 {
			headers[claudeHeaderPrefix+"reset"] = fmt.Sprintf("%d", reset7d)
		}
	case has5h:
		headers[claudeHeaderPrefix+"representative-claim"] = claudeClaim5h
		if reset5h > 0 {
			headers[claudeHeaderPrefix+"reset"] = fmt.Sprintf("%d", reset5h)
		}
	}

	// Overage is the wallet balance: the extra allowance that continues usage
	// after the subscription runs out. Claude Code has no balance field, so the
	// header only states availability, never a percentage.
	if wallet.Available {
		headers[claudeHeaderPrefix+"overage-status"] = "allowed"
	} else {
		headers[claudeHeaderPrefix+"overage-status"] = "rejected"
		headers[claudeHeaderPrefix+"overage-disabled-reason"] = "out_of_credits"
	}
	if wallet.InUse {
		headers[claudeHeaderPrefix+"overage-in-use"] = "true"
	}
}

// fillCodexUsageHeaders writes the x-<limit>-* family plus the credits block.
//
// Codex orders windows primary (shortest) then secondary; anything beyond two
// window lengths has no header to live in and is dropped rather than invented.
// used-percent is 0-100, window-minutes is minutes, reset-at is unix seconds.
func fillCodexUsageHeaders(headers map[string]string, windows []harnessUsageWindow, prefix string, balanceQuota int, now int64) {
	var global []harnessUsageWindow
	perGroup := make(map[string][]harnessUsageWindow)
	for _, window := range windows {
		if window.Group == "" {
			global = append(global, window)
			continue
		}
		perGroup[window.Group] = append(perGroup[window.Group], window)
	}

	writeCodexWindow := func(prefix string, window harnessUsageWindow, slot string) {
		headers[prefix+"-"+slot+"-used-percent"] = fmt.Sprintf("%.2f", window.utilization()*100)
		headers[prefix+"-"+slot+"-window-minutes"] = fmt.Sprintf("%d", window.WindowSeconds/60)
		if reset := WindowResetAt(window.WindowFrom, window.WindowSeconds, now); reset > 0 {
			headers[prefix+"-"+slot+"-reset-at"] = fmt.Sprintf("%d", reset)
		}
	}

	// Sort shortest first so primary is the tightest window, matching what
	// Codex expects from its own primary/secondary split.
	sortWindowsByLength(global)
	if len(global) > 0 {
		writeCodexWindow(prefix, global[0], "primary")
	}
	if len(global) > 1 {
		writeCodexWindow(prefix, global[1], "secondary")
	}
	for group, groupWindows := range perGroup {
		sortWindowsByLength(groupWindows)
		groupPrefix := prefix + "-" + sanitizeHeaderToken(group)
		writeCodexWindow(groupPrefix, groupWindows[0], "primary")
		if len(groupWindows) > 1 {
			writeCodexWindow(groupPrefix, groupWindows[1], "secondary")
		}
	}

	// Credits must be sent as a complete block: Codex only parses the snapshot
	// when has-credits and unlimited are both present.
	headers["x-codex-credits-has-credits"] = boolHeaderValue(balanceQuota > 0)
	headers["x-codex-credits-unlimited"] = "false"
	headers["x-codex-credits-balance"] = fmt.Sprintf("%.2f", float64(max(balanceQuota, 0))/common.QuotaPerUnit)
}

func sortWindowsByLength(windows []harnessUsageWindow) {
	// Insertion sort keeps this dependency-free and the slice is tiny.
	for i := 1; i < len(windows); i++ {
		for j := i; j > 0 && windows[j].WindowSeconds < windows[j-1].WindowSeconds; j-- {
			windows[j], windows[j-1] = windows[j-1], windows[j]
		}
	}
}

func boolHeaderValue(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// walletAvailable reports whether the wallet can still pay for a request.
func walletAvailable(info *relaycommon.RelayInfo) bool {
	return info != nil && info.UserQuota > 0
}

// ---------------------------------------------------------------------------
// GET /api/oauth/usage
// ---------------------------------------------------------------------------

// HarnessUsagePeriod is one usage window in the shape the Claude Code /usage
// page renders. Utilization is a 0-100 percentage (Anthropic's own unit for
// this endpoint, unlike the 0-1 fraction in the response headers); ResetsAt is
// unix seconds and omitted when the window has no known end.
type HarnessUsagePeriod struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    *int64  `json:"resets_at"`
}

// HarnessUsageResponse mirrors Anthropic's /usage body. Only the windows that
// exist are present; every other field is omitted rather than zero-filled, so
// the client shows no bar for an account that has no such cap.
type HarnessUsageResponse struct {
	FiveHour       *HarnessUsagePeriod `json:"five_hour,omitempty"`
	SevenDay       *HarnessUsagePeriod `json:"seven_day,omitempty"`
	SevenDayOpus   *HarnessUsagePeriod `json:"seven_day_opus,omitempty"`
	SevenDaySonnet *HarnessUsagePeriod `json:"seven_day_sonnet,omitempty"`
	// ExtraUsage is the wallet, the same "overage" the Claude response headers
	// report as allowed / rejected.
	ExtraUsage *HarnessExtraUsage `json:"extra_usage,omitempty"`
}

// HarnessExtraUsage is the wallet-backed overage block. Anthropic reports a
// dollar budget here; new-api only knows whether the wallet can still pay, so
// the budget fields stay absent and the availability is stated explicitly.
type HarnessExtraUsage struct {
	IsEnabled    bool    `json:"is_enabled"`
	MonthlyLimit *int64  `json:"monthly_limit,omitempty"`
	UsedCredits  *int64  `json:"used_credits,omitempty"`
	Utilization  float64 `json:"utilization"`
}

// BuildHarnessUsageResponse renders the same merged snapshot the response
// headers carry, in the JSON shape the /usage page reads. It returns nil when
// the feature or the user's subscriptions do not qualify, which the caller must
// treat as "no usage to report" rather than as an error.
func BuildHarnessUsageResponse(userId int, walletQuota int) *HarnessUsageResponse {
	if userId <= 0 {
		return nil
	}
	setting := operation_setting.GetHarnessUsageSetting()
	if setting == nil || !setting.Enabled {
		return nil
	}
	windows := collectHarnessUsageWindows(userId)
	if len(windows) == 0 {
		return nil
	}

	now := common.GetTimestamp()
	response := &HarnessUsageResponse{}
	for _, window := range windows {
		period := &HarnessUsagePeriod{Utilization: window.utilization() * 100}
		if resetAt := WindowResetAt(window.WindowFrom, window.WindowSeconds, now); resetAt > 0 {
			period.ResetsAt = &resetAt
		}
		if window.Group != "" {
			// Per-model 7d windows have dedicated fields for the two families
			// Anthropic names; any other group has no field and is dropped rather
			// than emitted under a key the page does not render.
			if window.WindowSeconds != model.SubscriptionLimitWindow7d {
				continue
			}
			switch window.Group {
			case "sonnet":
				response.SevenDaySonnet = period
			case "opus":
				response.SevenDayOpus = period
			}
			continue
		}
		switch window.WindowSeconds {
		case model.SubscriptionLimitWindow5h:
			response.FiveHour = period
		case model.SubscriptionLimitWindow7d:
			response.SevenDay = period
		}
	}

	response.ExtraUsage = &HarnessExtraUsage{IsEnabled: walletQuota > 0}
	if response.FiveHour == nil && response.SevenDay == nil &&
		response.SevenDaySonnet == nil && response.SevenDayOpus == nil {
		// The wallet alone is not a subscription quota window; reporting an empty
		// body would make the page claim the account has no usage data at all.
		return nil
	}
	return response
}
