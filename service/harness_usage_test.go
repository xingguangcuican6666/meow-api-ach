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
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedHarnessUsagePlan creates one plan with the given limits and one active
// subscription for userId, returning the subscription. Reporting is toggled by
// report so tests can prove the opt-in gate.
func seedHarnessUsagePlan(t *testing.T, userId int, report bool, limits []model.SubscriptionQuotaLimit, states []model.SubscriptionQuotaLimitState) *model.UserSubscription {
	t.Helper()
	enabled := report
	plan := &model.SubscriptionPlan{
		Title:                "harness test plan",
		PriceAmount:          1,
		Currency:             "USD",
		DurationUnit:         model.SubscriptionDurationMonth,
		DurationValue:        1,
		Enabled:              true,
		QuotaResetPeriod:     model.SubscriptionResetNever,
		ReportUsageToHarness: &enabled,
		QuotaLimits:          limits,
	}
	require.NoError(t, model.DB.Create(plan).Error)
	now := common.GetTimestamp()
	sub := &model.UserSubscription{
		UserId:           userId,
		PlanId:           plan.Id,
		AmountTotal:      1000,
		AmountUsed:       0,
		StartTime:        now - 60,
		EndTime:          now + 3600,
		Status:           "active",
		Source:           "order",
		QuotaLimitStates: states,
	}
	require.NoError(t, model.DB.Create(sub).Error)
	model.InvalidateSubscriptionPlanCache(plan.Id)
	t.Cleanup(func() {
		model.DB.Where("plan_id = ?", plan.Id).Delete(&model.UserSubscription{})
		model.DB.Delete(&model.SubscriptionPlan{}, plan.Id)
		model.InvalidateSubscriptionPlanCache(plan.Id)
	})
	return sub
}

func setHarnessUsageEnabled(t *testing.T, enabled bool) {
	t.Helper()
	setting := operation_setting.GetHarnessUsageSetting()
	previous := setting.Enabled
	setting.Enabled = enabled
	t.Cleanup(func() { setting.Enabled = previous })
}

// snapshotHeaders runs the snapshot against a throwaway gin context and returns
// the headers it produced.
func snapshotHeaders(t *testing.T, info *relaycommon.RelayInfo) map[string]string {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	SnapshotHarnessUsageHeaders(c, info)
	headers, _ := common.GetContextKeyType[map[string]string](c, "harness_usage_headers")
	return headers
}

func claudeRelayInfo(userId int, userQuota int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          userId,
		UserQuota:       userQuota,
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "claude-sonnet-4-5",
		RequestURLPath:  "/v1/messages",
	}
}

func codexRelayInfo(userId int, userQuota int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          userId,
		UserQuota:       userQuota,
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-5.2-codex",
		RequestURLPath:  "/v1/responses",
	}
}

func TestHarnessUsageHeaderFamilySelection(t *testing.T) {
	cases := []struct {
		name   string
		info   *relaycommon.RelayInfo
		expect types.RelayFormat
	}{
		{"claude relay format", &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude}, types.RelayFormatClaude},
		{"claude path", &relaycommon.RelayInfo{RequestURLPath: "/v1/messages?beta=true"}, types.RelayFormatClaude},
		{"claude model", &relaycommon.RelayInfo{OriginModelName: "claude-opus-4-1"}, types.RelayFormatClaude},
		{"codex relay format", &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses}, types.RelayFormatOpenAIResponses},
		{"codex path", &relaycommon.RelayInfo{RequestURLPath: "/v1/responses"}, types.RelayFormatOpenAIResponses},
		{"codex model", &relaycommon.RelayInfo{OriginModelName: "gpt-5.1"}, types.RelayFormatOpenAIResponses},
		{"o-series model", &relaycommon.RelayInfo{OriginModelName: "o3-mini"}, types.RelayFormatOpenAIResponses},
		{"embedding emits nothing", &relaycommon.RelayInfo{RelayFormat: types.RelayFormatEmbedding, RequestURLPath: "/v1/embeddings", OriginModelName: "text-embedding-3-large"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expect, harnessHeaderFamily(tc.info))
		})
	}
}

func TestHarnessUsageModelGroupInference(t *testing.T) {
	cases := []struct {
		pattern string
		key     string
		expect  string
	}{
		{"claude-sonnet-4-5", "limit-1", "sonnet"},
		{"claude-opus-4-1", "limit-1", "opus"},
		{"claude-fable-5-1", "limit-1", "fable"},
		{"gpt-5.2-codex", "limit-1", "codex"},
		{"claude-*", "model-cap", "claude"},
		{"gpt-4o", "fallback-key", "gpt"},
		{"2024-05-01", "fallback-key", "fallback-key"},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			assert.Equal(t, tc.expect, harnessModelGroup(tc.pattern, tc.key))
		})
	}
}

func TestHarnessUsageStripsOnlyQuotaHeaders(t *testing.T) {
	for _, name := range []string{
		"anthropic-ratelimit-unified-5h-utilization",
		"Anthropic-Ratelimit-Requests-Limit",
		"x-codex-primary-used-percent",
		"x-codex-secondary-window-minutes",
		"x-codex-primary-reset-at",
		"x-codex-limit-name",
		"x-codex-credits-balance",
		"x-codex-credits-has-credits",
		"x-codex-rate-limit-reached-type",
	} {
		assert.True(t, IsUpstreamUsageLimitHeader(name), "%s must be stripped", name)
	}
	for _, name := range []string{
		"X-Codex-Turn-State",
		"X-Codex-Beta-Features",
		"X-Codex-Window-Id",
		"X-Codex-Turn-Metadata",
		"X-Codex-Parent-Thread-Id",
		"X-Codex-Promo-Message",
		"Content-Type",
		"X-Oneapi-Request-Id",
	} {
		assert.False(t, IsUpstreamUsageLimitHeader(name), "%s must be preserved", name)
	}
}

func TestHarnessUsageClaudeHeaders(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980001
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "5h", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 100},
			{Key: "7d", WindowSeconds: model.SubscriptionLimitWindow7d, AmountTotal: 1000},
			{Key: "sonnet", WindowSeconds: model.SubscriptionLimitWindow7d, AmountTotal: 200, Models: []string{"claude-sonnet-*"}},
		},
		[]model.SubscriptionQuotaLimitState{
			{Key: "5h", Used: 25, WindowFrom: now - 100},
			{Key: "7d", Used: 500, WindowFrom: now - 100},
			{Key: "sonnet", Model: "claude-sonnet-4-5", Used: 50, WindowFrom: now - 100},
		},
	)
	setHarnessUsageEnabled(t, true)

	headers := snapshotHeaders(t, claudeRelayInfo(userId, int(5*common.QuotaPerUnit)))
	require.NotEmpty(t, headers)

	// Claude Code reads utilization as a 0-1 fraction with two decimals.
	assert.Equal(t, "0.25", headers["anthropic-ratelimit-unified-5h-utilization"])
	assert.Equal(t, "0.50", headers["anthropic-ratelimit-unified-7d-utilization"])
	assert.Equal(t, "0.25", headers["anthropic-ratelimit-unified-7d_sonnet-utilization"])
	assert.Equal(t, "allowed", headers["anthropic-ratelimit-unified-status"])
	assert.Equal(t, "7d", headers["anthropic-ratelimit-unified-representative-claim"])
	assert.NotEmpty(t, headers["anthropic-ratelimit-unified-7d-reset"])

	// Threshold and fallback headers would hijack Claude Code's own warnings.
	for key := range headers {
		assert.NotContains(t, key, "surpassed-threshold")
		assert.NotContains(t, key, "-fallback")
	}

	// Overage reports availability only, never a percentage.
	assert.Equal(t, "allowed", headers["anthropic-ratelimit-unified-overage-status"])
	assert.NotContains(t, headers, "anthropic-ratelimit-unified-overage-utilization")
	assert.NotContains(t, headers, "anthropic-ratelimit-unified-overage-disabled-reason")
}

func TestHarnessUsageClaudeOverageOutOfCredits(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980002
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "5h", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 100},
		},
		[]model.SubscriptionQuotaLimitState{{Key: "5h", Used: 10, WindowFrom: now - 10}},
	)
	setHarnessUsageEnabled(t, true)

	headers := snapshotHeaders(t, claudeRelayInfo(userId, 0))
	require.NotEmpty(t, headers)
	assert.Equal(t, "rejected", headers["anthropic-ratelimit-unified-overage-status"])
	assert.Equal(t, "out_of_credits", headers["anthropic-ratelimit-unified-overage-disabled-reason"])
	assert.NotContains(t, headers, "anthropic-ratelimit-unified-5h_sonnet-utilization")
}

func TestHarnessUsageCodexHeaders(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980003
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "5h", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 100},
			{Key: "7d", WindowSeconds: model.SubscriptionLimitWindow7d, AmountTotal: 1000},
			{Key: "codex", WindowSeconds: model.SubscriptionLimitWindow7d, AmountTotal: 200, Models: []string{"gpt-5.2-codex"}},
		},
		[]model.SubscriptionQuotaLimitState{
			{Key: "5h", Used: 25, WindowFrom: now - 100},
			{Key: "7d", Used: 500, WindowFrom: now - 100},
			{Key: "codex", Model: "gpt-5.2-codex", Used: 100, WindowFrom: now - 100},
		},
	)
	setHarnessUsageEnabled(t, true)

	headers := snapshotHeaders(t, codexRelayInfo(userId, int(2*common.QuotaPerUnit)))
	require.NotEmpty(t, headers)

	// Codex reads used-percent as 0-100 and the window in minutes.
	assert.Equal(t, "25.00", headers["x-codex-primary-used-percent"])
	assert.Equal(t, "300", headers["x-codex-primary-window-minutes"])
	assert.Equal(t, "50.00", headers["x-codex-secondary-used-percent"])
	assert.Equal(t, "10080", headers["x-codex-secondary-window-minutes"])
	assert.Equal(t, "50.00", headers["x-codex-codex-primary-used-percent"])

	// Credits must be a complete block for Codex to parse it at all.
	assert.Equal(t, "true", headers["x-codex-credits-has-credits"])
	assert.Equal(t, "false", headers["x-codex-credits-unlimited"])
	assert.Equal(t, "2.00", headers["x-codex-credits-balance"])

	// No Claude family leaks into a Codex response.
	for key := range headers {
		assert.NotContains(t, key, "anthropic-ratelimit-")
	}
}

func TestHarnessUsageMergesActiveSubscriptions(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980004
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "5h", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 100},
		},
		[]model.SubscriptionQuotaLimitState{{Key: "5h", Used: 25, WindowFrom: now - 100}},
	)
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "5h", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 300},
		},
		[]model.SubscriptionQuotaLimitState{{Key: "5h", Used: 50, WindowFrom: now - 100}},
	)
	setHarnessUsageEnabled(t, true)

	headers := snapshotHeaders(t, claudeRelayInfo(userId, 0))
	// 75 used of 400 total across both subscriptions.
	assert.Equal(t, "0.19", headers["anthropic-ratelimit-unified-5h-utilization"])
}

func TestHarnessUsageSkipsNonOptedInPlans(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980005
	seedHarnessUsagePlan(t, userId, false,
		[]model.SubscriptionQuotaLimit{
			{Key: "5h", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 100},
		},
		[]model.SubscriptionQuotaLimitState{{Key: "5h", Used: 25, WindowFrom: now - 100}},
	)
	setHarnessUsageEnabled(t, true)

	assert.Empty(t, snapshotHeaders(t, claudeRelayInfo(userId, 0)))
}

func TestHarnessUsageGlobalSwitchOffEmitsNothing(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980006
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "5h", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 100},
		},
		[]model.SubscriptionQuotaLimitState{{Key: "5h", Used: 25, WindowFrom: now - 100}},
	)
	setHarnessUsageEnabled(t, false)

	assert.Empty(t, snapshotHeaders(t, claudeRelayInfo(userId, 0)))
	assert.Empty(t, snapshotHeaders(t, codexRelayInfo(userId, 0)))
}

func TestHarnessUsageMissingWindowIsOmitted(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980007
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "7d", WindowSeconds: model.SubscriptionLimitWindow7d, AmountTotal: 1000},
		},
		[]model.SubscriptionQuotaLimitState{{Key: "7d", Used: 500, WindowFrom: now - 100}},
	)
	setHarnessUsageEnabled(t, true)

	headers := snapshotHeaders(t, claudeRelayInfo(userId, 0))
	assert.NotContains(t, headers, "anthropic-ratelimit-unified-5h-utilization")
	assert.Equal(t, "0.50", headers["anthropic-ratelimit-unified-7d-utilization"])
}

func TestHarnessUsageWindowRollsWithoutPersisting(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980008
	sub := seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "5h", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 100},
		},
		[]model.SubscriptionQuotaLimitState{
			{Key: "5h", Used: 80, WindowFrom: now - model.SubscriptionLimitWindow5h - 10},
		},
	)
	setHarnessUsageEnabled(t, true)

	headers := snapshotHeaders(t, claudeRelayInfo(userId, 0))
	assert.Equal(t, "0.00", headers["anthropic-ratelimit-unified-5h-utilization"])

	// Reporting must never write the rolled counter back.
	var reloaded model.UserSubscription
	require.NoError(t, model.DB.Where("id = ?", sub.Id).First(&reloaded).Error)
	require.Len(t, reloaded.QuotaLimitStates, 1)
	assert.EqualValues(t, 80, reloaded.QuotaLimitStates[0].Used)
}

func TestHarnessUsageCodexOverageInUse(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980009
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "5h", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 100},
		},
		[]model.SubscriptionQuotaLimitState{{Key: "5h", Used: 100, WindowFrom: now - 100}},
	)
	setHarnessUsageEnabled(t, true)

	info := claudeRelayInfo(userId, int(3*common.QuotaPerUnit))
	info.BillingSource = BillingSourceWallet
	headers := snapshotHeaders(t, info)
	assert.Equal(t, "true", headers["anthropic-ratelimit-unified-overage-in-use"])
}

func TestWriteHarnessUsageHeadersIsNoopWithoutSnapshot(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	WriteHarnessUsageHeaders(c)
	assert.Empty(t, recorder.Header())
}

// ---------------------------------------------------------------------------
// GET /api/oauth/usage payload
// ---------------------------------------------------------------------------

// The /usage page reads percentages (0-100), not the 0-1 fraction the response
// headers carry. Both must come from the same merged windows so a user never
// sees two different numbers for one quota.
func TestHarnessUsageResponseUsesPercentages(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980101
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "h5", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 200},
			{Key: "d7", WindowSeconds: model.SubscriptionLimitWindow7d, AmountTotal: 400},
		},
		[]model.SubscriptionQuotaLimitState{
			{Key: "h5", Used: 50, WindowFrom: now - 100},
			{Key: "d7", Used: 100, WindowFrom: now - 200},
		},
	)
	setHarnessUsageEnabled(t, true)

	response := BuildHarnessUsageResponse(userId, int(2*common.QuotaPerUnit))
	require.NotNil(t, response)
	require.NotNil(t, response.FiveHour)
	require.NotNil(t, response.SevenDay)
	assert.InDelta(t, 25, response.FiveHour.Utilization, 0.001)
	assert.InDelta(t, 25, response.SevenDay.Utilization, 0.001)
	require.NotNil(t, response.FiveHour.ResetsAt)
	assert.EqualValues(t, now-100+model.SubscriptionLimitWindow5h, *response.FiveHour.ResetsAt)
	require.NotNil(t, response.ExtraUsage)
	assert.True(t, response.ExtraUsage.IsEnabled)
}

// A user without an opted-in subscription gets no payload at all: the page must
// not render zeros that read as an exhausted quota.
func TestHarnessUsageResponseEmptyWithoutOptIn(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980102
	seedHarnessUsagePlan(t, userId, false,
		[]model.SubscriptionQuotaLimit{
			{Key: "h5", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 200},
		},
		[]model.SubscriptionQuotaLimitState{{Key: "h5", Used: 50, WindowFrom: now}},
	)
	setHarnessUsageEnabled(t, true)

	assert.Nil(t, BuildHarnessUsageResponse(userId, int(2*common.QuotaPerUnit)))
}

func TestHarnessUsageResponseEmptyWhenMasterSwitchIsOff(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980103
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "h5", WindowSeconds: model.SubscriptionLimitWindow5h, AmountTotal: 200},
		},
		[]model.SubscriptionQuotaLimitState{{Key: "h5", Used: 50, WindowFrom: now}},
	)
	setHarnessUsageEnabled(t, false)

	assert.Nil(t, BuildHarnessUsageResponse(userId, int(2*common.QuotaPerUnit)))
}

// Only the per-model families Anthropic names have a field, and only inside a 7d
// window. An 11h per-model cap would otherwise be reported under a 7d field.
func TestHarnessUsageResponsePerModelFields(t *testing.T) {
	now := common.GetTimestamp()
	userId := 980104
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "d7", WindowSeconds: model.SubscriptionLimitWindow7d, AmountTotal: 400},
			{
				Key:           "sonnet",
				WindowSeconds: model.SubscriptionLimitWindow7d,
				AmountTotal:   100,
				Models:        []string{"claude-sonnet-*"},
			},
			{
				Key:           "opus",
				WindowSeconds: model.SubscriptionLimitWindow5h,
				AmountTotal:   100,
				Models:        []string{"claude-opus-*"},
			},
		},
		[]model.SubscriptionQuotaLimitState{
			{Key: "d7", Used: 100, WindowFrom: now - 200},
			{Key: "sonnet", Model: "claude-sonnet-4-5", Used: 50, WindowFrom: now - 200},
			{Key: "opus", Model: "claude-opus-4-1", Used: 50, WindowFrom: now - 200},
		},
	)
	setHarnessUsageEnabled(t, true)

	response := BuildHarnessUsageResponse(userId, 0)
	require.NotNil(t, response)
	require.NotNil(t, response.SevenDaySonnet)
	assert.InDelta(t, 50, response.SevenDaySonnet.Utilization, 0.001)
	assert.Nil(t, response.SevenDayOpus, "a 5h per-model cap has no 7d field to live in")
	require.NotNil(t, response.ExtraUsage)
	assert.False(t, response.ExtraUsage.IsEnabled, "an empty wallet means overage is unavailable")
}

// A user whose only opted-in plan has no windowed limits has nothing to plot;
// the wallet alone must not produce a body.
func TestHarnessUsageResponseIgnoresWalletOnly(t *testing.T) {
	userId := 980105
	seedHarnessUsagePlan(t, userId, true,
		[]model.SubscriptionQuotaLimit{
			{Key: "claude", AmountTotal: 100, Models: []string{"claude-*"}},
		},
		nil,
	)
	setHarnessUsageEnabled(t, true)

	assert.Nil(t, BuildHarnessUsageResponse(userId, int(5*common.QuotaPerUnit)))
}
