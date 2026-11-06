package model

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// quotaLimitPlan builds a plan with the three caps the feature must support at
// the same time: a 5 hour window, a 7 day window and an independent quota for
// one model family.
func quotaLimitPlan(id int) *SubscriptionPlan {
	return &SubscriptionPlan{
		Id:               id,
		Title:            "Limits",
		PriceAmount:      10,
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      0, // unlimited whole-period quota
		QuotaResetPeriod: SubscriptionResetNever,
		QuotaLimits: SubscriptionQuotaLimits{
			{Key: "h5", Name: "5h", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100},
			{Key: "d7", Name: "7d", WindowSeconds: SubscriptionLimitWindow7d, AmountTotal: 1000},
			{Key: "claude", Name: "Claude", AmountTotal: 50, Models: []string{"claude-*"}},
		},
	}
}

// purgeSubscriptionPlanCache empties the process-global plan cache so a test
// that recreates a plan id never reads the previous run's row.
func purgeSubscriptionPlanCache(t *testing.T) {
	t.Helper()
	_ = getSubscriptionPlanCache().Purge()
	_ = getSubscriptionPlanInfoCache().Purge()
}

func seedQuotaLimitPlan(t *testing.T, plan *SubscriptionPlan) *SubscriptionPlan {
	t.Helper()
	purgeSubscriptionPlanCache(t)
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func seedQuotaLimitSubscription(t *testing.T, userId int, planId int, amountTotal int64) int {
	t.Helper()
	now := GetDBTimestamp()
	sub := &UserSubscription{
		UserId:      userId,
		PlanId:      planId,
		AmountTotal: amountTotal,
		StartTime:   now - 60,
		EndTime:     now + 30*24*3600,
		Status:      "active",
	}
	require.NoError(t, DB.Create(sub).Error)
	return sub.Id
}

func loadQuotaLimitSubscription(t *testing.T, id int) UserSubscription {
	t.Helper()
	var sub UserSubscription
	require.NoError(t, DB.Where("id = ?", id).First(&sub).Error)
	return sub
}

func quotaLimitState(t *testing.T, subId int, key string, modelName string) (SubscriptionQuotaLimitState, bool) {
	t.Helper()
	sub := loadQuotaLimitSubscription(t, subId)
	state, index := findSubscriptionLimitState(sub.QuotaLimitStates, key, modelName)
	return state, index >= 0
}

// The plan-level caps are enforced during pre-consume: exhausting the 5 hour
// window blocks a request while the 7 day window still has room.
func TestPreConsumeUserSubscriptionEnforcesEveryConfiguredLimit(t *testing.T) {
	truncateTables(t)

	plan := seedQuotaLimitPlan(t, quotaLimitPlan(9301))
	subId := seedQuotaLimitSubscription(t, 301, plan.Id, plan.TotalAmount)

	res, err := PreConsumeUserSubscription("limit-req-1", 301, "gpt-4o", 0, 40)
	require.NoError(t, err)
	assert.Equal(t, subId, res.UserSubscriptionId)
	assert.Equal(t, []types.SubscriptionLimitRef{
		{Key: "h5"},
		{Key: "d7"},
	}, res.LimitRefs)

	h5, ok := quotaLimitState(t, subId, "h5", "")
	require.True(t, ok)
	assert.EqualValues(t, 40, h5.Used)
	assert.NotZero(t, h5.WindowFrom)
	d7, ok := quotaLimitState(t, subId, "d7", "")
	require.True(t, ok)
	assert.EqualValues(t, 40, d7.Used)

	// 100 total per 5h: a second 40 fits, a third 40 does not.
	_, err = PreConsumeUserSubscription("limit-req-2", 301, "gpt-4o", 0, 40)
	require.NoError(t, err)
	_, err = PreConsumeUserSubscription("limit-req-3", 301, "gpt-4o", 0, 40)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscription quota insufficient")
	assert.Contains(t, err.Error(), "5h")

	// The 7 day window still has room, so a cheaper request is admitted.
	res, err = PreConsumeUserSubscription("limit-req-4", 301, "gpt-4o", 0, 10)
	require.NoError(t, err)
	assert.EqualValues(t, 10, res.PreConsumed)
	h5, _ = quotaLimitState(t, subId, "h5", "")
	assert.EqualValues(t, 90, h5.Used)
	d7, _ = quotaLimitState(t, subId, "d7", "")
	assert.EqualValues(t, 90, d7.Used)
}

// A model-scoped limit keeps one counter per matched model, so the two Claude
// variants do not consume each other's quota, and a non-matching model is not
// charged on that limit at all.
func TestPreConsumeUserSubscriptionKeepsIndependentPerModelCounters(t *testing.T) {
	truncateTables(t)

	plan := seedQuotaLimitPlan(t, quotaLimitPlan(9302))
	subId := seedQuotaLimitSubscription(t, 302, plan.Id, plan.TotalAmount)

	res, err := PreConsumeUserSubscription("model-req-1", 302, "claude-sonnet-4", 0, 30)
	require.NoError(t, err)
	assert.Equal(t, []types.SubscriptionLimitRef{
		{Key: "h5"},
		{Key: "d7"},
		{Key: "claude", Model: "claude-sonnet-4"},
	}, res.LimitRefs)

	sonnet, ok := quotaLimitState(t, subId, "claude", "claude-sonnet-4")
	require.True(t, ok)
	assert.EqualValues(t, 30, sonnet.Used)
	// No counter for a model that was never requested.
	_, ok = quotaLimitState(t, subId, "claude", "claude-opus-4")
	assert.False(t, ok)

	_, err = PreConsumeUserSubscription("model-req-2", 302, "claude-opus-4", 0, 30)
	require.NoError(t, err)
	opus, ok := quotaLimitState(t, subId, "claude", "claude-opus-4")
	require.True(t, ok)
	assert.EqualValues(t, 30, opus.Used)

	// sonnet is at 30 of 50 and opus at 30 of 50: a third 30-unit request on
	// either model is rejected while the shared windows still have room.
	_, err = PreConsumeUserSubscription("model-req-3", 302, "claude-sonnet-4", 0, 30)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "claude-sonnet-4")

	_, err = PreConsumeUserSubscription("model-req-4", 302, "gpt-4o", 0, 30)
	require.NoError(t, err)
	_, ok = quotaLimitState(t, subId, "claude", "gpt-4o")
	assert.False(t, ok, "a model outside the limit must not get a counter")
}

// Settlement must adjust the counters the request reserved on, and a refund
// must release them again. Otherwise a settled request would leave its 5h and
// per-model usage at the pre-consumed amount only.
func TestPostConsumeUserSubscriptionDeltaWithLimitsAdjustsReservedCounters(t *testing.T) {
	truncateTables(t)

	plan := seedQuotaLimitPlan(t, quotaLimitPlan(9303))
	subId := seedQuotaLimitSubscription(t, 303, plan.Id, plan.TotalAmount)

	res, err := PreConsumeUserSubscription("settle-req-1", 303, "claude-sonnet-4", 0, 20)
	require.NoError(t, err)

	// Actual cost was 35: settle adds 15 to both the total and every counter.
	require.NoError(t, PostConsumeUserSubscriptionDeltaWithLimits(subId, 15, res.LimitRefs))
	sub := loadQuotaLimitSubscription(t, subId)
	assert.EqualValues(t, 35, sub.AmountUsed)
	h5, _ := quotaLimitState(t, subId, "h5", "")
	assert.EqualValues(t, 35, h5.Used)
	d7, _ := quotaLimitState(t, subId, "d7", "")
	assert.EqualValues(t, 35, d7.Used)
	claude, _ := quotaLimitState(t, subId, "claude", "claude-sonnet-4")
	assert.EqualValues(t, 35, claude.Used)

	// A negative settle releases usage but never drives a counter below zero.
	require.NoError(t, PostConsumeUserSubscriptionDeltaWithLimits(subId, -100, res.LimitRefs))
	sub = loadQuotaLimitSubscription(t, subId)
	assert.EqualValues(t, 0, sub.AmountUsed)
	h5, _ = quotaLimitState(t, subId, "h5", "")
	assert.EqualValues(t, 0, h5.Used)

	// The legacy entry point still works and leaves the counters untouched.
	require.NoError(t, PostConsumeUserSubscriptionDelta(subId, 10))
	h5, _ = quotaLimitState(t, subId, "h5", "")
	assert.EqualValues(t, 0, h5.Used)
}

// A refund by request id must release exactly the counters that request
// reserved, read back from the persisted record rather than from memory.
func TestRefundSubscriptionPreConsumeReleasesLimitCounters(t *testing.T) {
	truncateTables(t)

	plan := seedQuotaLimitPlan(t, quotaLimitPlan(9304))
	subId := seedQuotaLimitSubscription(t, 304, plan.Id, plan.TotalAmount)

	_, err := PreConsumeUserSubscription("refund-req-1", 304, "claude-sonnet-4", 0, 50)
	require.NoError(t, err)
	claude, _ := quotaLimitState(t, subId, "claude", "claude-sonnet-4")
	require.EqualValues(t, 50, claude.Used)

	require.NoError(t, RefundSubscriptionPreConsume("refund-req-1"))
	// Idempotent: a second refund must not release anything twice.
	require.NoError(t, RefundSubscriptionPreConsume("refund-req-1"))

	sub := loadQuotaLimitSubscription(t, subId)
	assert.EqualValues(t, 0, sub.AmountUsed)
	claude, _ = quotaLimitState(t, subId, "claude", "claude-sonnet-4")
	assert.EqualValues(t, 0, claude.Used)
	h5, _ := quotaLimitState(t, subId, "h5", "")
	assert.EqualValues(t, 0, h5.Used)

	// The released room is usable again inside the same window.
	_, err = PreConsumeUserSubscription("refund-req-2", 304, "claude-sonnet-4", 0, 50)
	require.NoError(t, err)
}

// An oversized request must be rejected by the cap rather than wrapping the
// int64 sum of used+amount past it. The room test subtracts instead of adding.
func TestPreConsumeUserSubscriptionRejectsOversizedAmountWithoutOverflow(t *testing.T) {
	truncateTables(t)

	plan := seedQuotaLimitPlan(t, &SubscriptionPlan{
		Id: 9310, Title: "Bounded", DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount: 0, QuotaResetPeriod: SubscriptionResetNever,
		QuotaLimits: SubscriptionQuotaLimits{
			{Key: "h5", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100},
		},
	})
	subId := seedQuotaLimitSubscription(t, 310, plan.Id, plan.TotalAmount)

	// Used is inside the cap; the request is astronomically larger than it.
	now := GetDBTimestamp()
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subId).
		Update("quota_limit_states", SubscriptionQuotaLimitStates{
			{Key: "h5", Used: 10, WindowFrom: now},
		}).Error)

	// math.MaxInt64 makes used+amount wrap negative, which an additive cap
	// test would read as "plenty of room left".
	_, err := PreConsumeUserSubscription("overflow-req-1", 310, "gpt-4o", 0, math.MaxInt64)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscription quota insufficient")

	stored := loadQuotaLimitSubscription(t, subId)
	h5, index := findSubscriptionLimitState(stored.QuotaLimitStates, "h5", "")
	require.True(t, index >= 0)
	assert.EqualValues(t, 10, h5.Used, "a rejected request must not charge the counter")
	assert.EqualValues(t, 0, stored.AmountUsed)
}

// A rolling window restarts on its own anchor: usage inside the previous 5h
// window must not count against the new one, while the untouched 7d window
// keeps accumulating.
func TestQuotaLimitWindowRollsIndependentlyPerLimit(t *testing.T) {
	truncateTables(t)

	plan := seedQuotaLimitPlan(t, quotaLimitPlan(9305))
	subId := seedQuotaLimitSubscription(t, 305, plan.Id, plan.TotalAmount)
	now := GetDBTimestamp()

	// 90 used in a window that already elapsed, and a 7d window that is still open.
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subId).
		Update("quota_limit_states", SubscriptionQuotaLimitStates{
			{Key: "h5", Used: 90, WindowFrom: now - SubscriptionLimitWindow5h - 1},
			{Key: "d7", Used: 900, WindowFrom: now - 3600},
		}).Error)

	res, err := PreConsumeUserSubscription("window-req-1", 305, "gpt-4o", 0, 80)
	require.NoError(t, err, "the elapsed 5h window must have restarted")
	assert.EqualValues(t, 80, res.PreConsumed)

	h5, _ := quotaLimitState(t, subId, "h5", "")
	assert.EqualValues(t, 80, h5.Used)
	assert.GreaterOrEqual(t, h5.WindowFrom, now)
	d7, _ := quotaLimitState(t, subId, "d7", "")
	assert.EqualValues(t, 980, d7.Used)
	assert.EqualValues(t, now-3600, d7.WindowFrom)

	// Filling the restarted 5h window blocks again.
	_, err = PreConsumeUserSubscription("window-req-2", 305, "gpt-4o", 0, 40)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "5h")
}

// Reading a subscription must roll windows in the response without writing the
// rolled counters, and must expose one entry per per-model counter.
func TestBuildSubscriptionLimitUsageRollsWithoutPersisting(t *testing.T) {
	truncateTables(t)

	plan := seedQuotaLimitPlan(t, quotaLimitPlan(9306))
	subId := seedQuotaLimitSubscription(t, 306, plan.Id, plan.TotalAmount)
	now := GetDBTimestamp()
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subId).
		Update("quota_limit_states", SubscriptionQuotaLimitStates{
			{Key: "h5", Used: 90, WindowFrom: now - SubscriptionLimitWindow5h - 1},
			{Key: "d7", Used: 100, WindowFrom: now - 60},
			{Key: "claude", Model: "claude-sonnet-4", Used: 20},
		}).Error)

	sub := loadQuotaLimitSubscription(t, subId)
	usage := buildSubscriptionLimitUsage(plan, &sub, now)
	// One row per windowed limit plus one row per per-model counter.
	require.Len(t, usage, 3)
	assert.Equal(t, "h5", usage[0].Key)
	assert.EqualValues(t, 0, usage[0].AmountUsed)
	assert.EqualValues(t, 100, usage[0].AmountRemain)
	assert.Equal(t, "d7", usage[1].Key)
	assert.EqualValues(t, 100, usage[1].AmountUsed)
	assert.EqualValues(t, now-60+SubscriptionLimitWindow7d, usage[1].NextResetTime)
	assert.Equal(t, "claude", usage[2].Key)
	assert.Equal(t, "claude-sonnet-4", usage[2].Model)
	assert.EqualValues(t, 20, usage[2].AmountUsed)
	assert.EqualValues(t, 30, usage[2].AmountRemain)
	assert.Zero(t, usage[2].NextResetTime, "a window-less limit has no rolling reset")

	// The summaries the APIs return carry the same rolled values.
	summaries, err := GetAllActiveUserSubscriptions(306)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Len(t, summaries[0].QuotaLimits, 3)
	assert.EqualValues(t, 0, summaries[0].QuotaLimits[0].AmountUsed)

	// Nothing was persisted by the read.
	stored := loadQuotaLimitSubscription(t, subId)
	h5, ok := findSubscriptionLimitState(stored.QuotaLimitStates, "h5", "")
	require.True(t, ok >= 0)
	assert.EqualValues(t, 90, h5.Used)
}

// Admin reset zeroes the counters that follow the plan reset cycle and leaves a
// running window alone, so an operator cannot shorten a 5h allowance.
func TestResetUserSubscriptionKeepsRunningWindowCounters(t *testing.T) {
	truncateTables(t)

	plan := seedQuotaLimitPlan(t, &SubscriptionPlan{
		Id:                      9307,
		Title:                   "Reset",
		PriceAmount:             10,
		DurationUnit:            SubscriptionDurationMonth,
		DurationValue:           1,
		TotalAmount:             500,
		QuotaResetPeriod:        SubscriptionResetDaily,
		QuotaResetCustomSeconds: 0,
		QuotaLimits: SubscriptionQuotaLimits{
			{Key: "h5", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100},
			{Key: "cycle", AmountTotal: 200},
		},
	})
	subId := seedQuotaLimitSubscription(t, 307, plan.Id, plan.TotalAmount)
	now := GetDBTimestamp()
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subId).
		Update("quota_limit_states", SubscriptionQuotaLimitStates{
			{Key: "h5", Used: 60, WindowFrom: now - 60},
			{Key: "cycle", Used: 150},
		}).Error)

	result, err := AdminResetUserSubscriptionsByPlan(307, plan.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 1, result.ResetCount)

	h5, _ := quotaLimitState(t, subId, "h5", "")
	assert.EqualValues(t, 60, h5.Used, "a rolling window must survive an admin reset")
	assert.EqualValues(t, now-60, h5.WindowFrom)
	cycle, _ := quotaLimitState(t, subId, "cycle", "")
	assert.EqualValues(t, 0, cycle.Used)
	sub := loadQuotaLimitSubscription(t, subId)
	assert.EqualValues(t, 0, sub.AmountUsed)
}

// Admin input must be validated before it becomes a billing cap: a limit needs
// a positive amount, keys must stay unique, and the count is bounded.
func TestValidateSubscriptionQuotaLimits(t *testing.T) {
	testCases := []struct {
		name    string
		input   []SubscriptionQuotaLimit
		want    []SubscriptionQuotaLimit
		wantErr error
	}{
		{
			name:  "empty list stores no column",
			input: []SubscriptionQuotaLimit{},
			want:  nil,
		},
		{
			name:  "assigns keys, trims models and drops duplicates",
			input: []SubscriptionQuotaLimit{{Name: " 5h ", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100, Models: []string{" claude-* ", "claude-*", "  ", "gpt-4o"}}},
			want:  []SubscriptionQuotaLimit{{Key: "limit-1", Name: "5h", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100, Models: []string{"claude-*", "gpt-4o"}}},
		},
		{
			name:    "rejects a non-positive amount",
			input:   []SubscriptionQuotaLimit{{Key: "h5", AmountTotal: 0}},
			wantErr: ErrSubscriptionQuotaLimitAmount,
		},
		{
			name:    "rejects a negative window",
			input:   []SubscriptionQuotaLimit{{Key: "h5", WindowSeconds: -1, AmountTotal: 10}},
			wantErr: ErrSubscriptionQuotaLimitWindow,
		},
		{
			name:    "rejects duplicate keys",
			input:   []SubscriptionQuotaLimit{{Key: "h5", AmountTotal: 10}, {Key: "h5", AmountTotal: 20}},
			wantErr: ErrSubscriptionQuotaLimitDuplicateKey,
		},
		{
			name:    "rejects blank-only model list",
			input:   []SubscriptionQuotaLimit{{Key: "h5", AmountTotal: 10, Models: []string{"  "}}},
			wantErr: ErrSubscriptionQuotaLimitModels,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateSubscriptionQuotaLimits(tc.input)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	// A cap above the JavaScript-safe quota domain the web UI renders is
	// rejected rather than stored, so a counter can never approach an int64
	// wrap when deltas accumulate.
	_, err := ValidateSubscriptionQuotaLimits([]SubscriptionQuotaLimit{
		{Key: "huge", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: common.MaxWalletQuota + 1},
	})
	assert.ErrorIs(t, err, ErrSubscriptionQuotaLimitAmount)
	normalized, err := ValidateSubscriptionQuotaLimits([]SubscriptionQuotaLimit{
		{Key: "huge", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: common.MaxWalletQuota},
	})
	require.NoError(t, err)
	require.Len(t, normalized, 1)
	assert.EqualValues(t, common.MaxWalletQuota, normalized[0].AmountTotal)

	tooMany := make([]SubscriptionQuotaLimit, 0, subscriptionQuotaLimitsMax+1)
	for range subscriptionQuotaLimitsMax + 1 {
		tooMany = append(tooMany, SubscriptionQuotaLimit{AmountTotal: 10})
	}
	_, err = ValidateSubscriptionQuotaLimits(tooMany)
	assert.ErrorIs(t, err, ErrSubscriptionQuotaLimitsTooMany)
}

// The limit definitions and the counters live in text/JSON columns that must
// survive a round trip through the driver and tolerate legacy NULL rows.
func TestSubscriptionQuotaLimitColumnsRoundTrip(t *testing.T) {
	truncateTables(t)

	limits, err := ValidateSubscriptionQuotaLimits([]SubscriptionQuotaLimit{
		{Name: "5h", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100},
		{Name: "7d", WindowSeconds: SubscriptionLimitWindow7d, AmountTotal: 1000},
		{Name: "模型独立", AmountTotal: 50, Models: []string{"claude-*"}},
	})
	require.NoError(t, err)
	plan := &SubscriptionPlan{
		Id:               9308,
		Title:            "Round trip",
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		TotalAmount:      0,
		QuotaResetPeriod: SubscriptionResetNever,
		QuotaLimits:      limits,
	}
	require.NoError(t, DB.Create(plan).Error)

	stored, err := GetSubscriptionPlanById(plan.Id)
	require.NoError(t, err)
	require.Len(t, stored.QuotaLimits, 3)
	assert.EqualValues(t, SubscriptionLimitWindow5h, stored.QuotaLimits[0].WindowSeconds)
	assert.EqualValues(t, SubscriptionLimitWindow7d, stored.QuotaLimits[1].WindowSeconds)
	assert.Equal(t, []string{"claude-*"}, stored.QuotaLimits[2].Models)

	subId := seedQuotaLimitSubscription(t, 308, plan.Id, plan.TotalAmount)
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subId).
		Update("quota_limit_states", SubscriptionQuotaLimitStates{
			{Key: "limit-1", Used: 10, WindowFrom: GetDBTimestamp()},
			{Key: "limit-3", Model: "claude-sonnet-4", Used: 20},
		}).Error)

	reloaded := loadQuotaLimitSubscription(t, subId)
	require.Len(t, reloaded.QuotaLimitStates, 2)
	assert.EqualValues(t, 10, reloaded.QuotaLimitStates[0].Used)
	assert.Equal(t, "claude-sonnet-4", reloaded.QuotaLimitStates[1].Model)

	// A row written before this feature has NULL in both columns.
	require.NoError(t, DB.Exec("UPDATE user_subscriptions SET quota_limit_states = NULL WHERE id = ?", subId).Error)
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Update("quota_limits", nil).Error)
	var legacySub UserSubscription
	require.NoError(t, DB.Where("id = ?", subId).First(&legacySub).Error)
	assert.Nil(t, legacySub.QuotaLimitStates)
	var legacyPlan SubscriptionPlan
	require.NoError(t, DB.Where("id = ?", plan.Id).First(&legacyPlan).Error)
	assert.Nil(t, legacyPlan.QuotaLimits)
	assert.Nil(t, legacyPlan.GetQuotaLimits())

	// Refs persist inside the pre-consume record so a later refund releases the
	// same counters.
	require.NoError(t, DB.Create(&SubscriptionPreConsumeRecord{
		RequestId:          "round-trip-req",
		UserId:             308,
		UserSubscriptionId: subId,
		PreConsumed:        5,
		Status:             "consumed",
		LimitRefs:          SubscriptionLimitRefs{{Key: "limit-1"}, {Key: "limit-3", Model: "claude-sonnet-4"}},
	}).Error)
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", "round-trip-req").First(&record).Error)
	require.Len(t, record.LimitRefs, 2)
	assert.Equal(t, "claude-sonnet-4", record.LimitRefs[1].Model)
}

// Model patterns are matched against the billing model name; the wildcard forms
// must not accidentally match unrelated models.
func TestMatchSubscriptionModelPattern(t *testing.T) {
	testCases := []struct {
		pattern string
		model   string
		want    bool
	}{
		{pattern: "claude-*", model: "claude-sonnet-4", want: true},
		{pattern: "claude-*", model: "gpt-4o", want: false},
		{pattern: "*-4o", model: "gpt-4o", want: true},
		{pattern: "*-4o", model: "gpt-4o-mini", want: false},
		{pattern: "gpt-4o", model: "gpt-4o", want: true},
		{pattern: "gpt-4o", model: "gpt-4o-mini", want: false},
		{pattern: "  claude-* ", model: "claude-opus-4", want: true},
		{pattern: "", model: "gpt-4o", want: false},
		{pattern: "claude-*", model: "  ", want: false},
	}
	for _, tc := range testCases {
		t.Run(tc.pattern+"/"+tc.model, func(t *testing.T) {
			assert.Equal(t, tc.want, matchSubscriptionModelPattern(tc.pattern, tc.model))
		})
	}
}

// The counters must be charged and released in the same units the request used,
// and a plan edited between pre-consume and settle must not corrupt them.
func TestSubscriptionLimitCountersSurvivePlanEdit(t *testing.T) {
	truncateTables(t)

	plan := seedQuotaLimitPlan(t, quotaLimitPlan(9309))
	subId := seedQuotaLimitSubscription(t, 309, plan.Id, plan.TotalAmount)

	res, err := PreConsumeUserSubscription("edit-req-1", 309, "claude-sonnet-4", 0, 30)
	require.NoError(t, err)

	// The operator raises the 5h cap and removes the model limit while the
	// request is in flight.
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Update("quota_limits", SubscriptionQuotaLimits{
			{Key: "h5", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 500},
			{Key: "d7", WindowSeconds: SubscriptionLimitWindow7d, AmountTotal: 1000},
		}).Error)
	purgeSubscriptionPlanCache(t)

	require.NoError(t, PostConsumeUserSubscriptionDeltaWithLimits(subId, 10, res.LimitRefs))
	sub := loadQuotaLimitSubscription(t, subId)
	assert.EqualValues(t, 40, sub.AmountUsed)

	// The removed limit's counter is dropped instead of growing forever, and the
	// surviving ones hold the settled value.
	_, ok := findSubscriptionLimitState(sub.QuotaLimitStates, "claude", "claude-sonnet-4")
	assert.Equal(t, -1, ok)
	h5, index := findSubscriptionLimitState(sub.QuotaLimitStates, "h5", "")
	require.True(t, index >= 0)
	assert.EqualValues(t, 40, h5.Used)

	// A refund of the removed limit is a no-op, not an error.
	require.NoError(t, PostConsumeUserSubscriptionDeltaWithLimits(subId, -5, []types.SubscriptionLimitRef{{Key: "claude", Model: "claude-sonnet-4"}}))
	h5, _ = quotaLimitState(t, subId, "h5", "")
	assert.EqualValues(t, 40, h5.Used)
}

// ---------------------------------------------------------------------------
// Schema migration across SQLite, MySQL and PostgreSQL
// ---------------------------------------------------------------------------

// subscriptionPlanV1 / userSubscriptionV1 / subscriptionPreConsumeRecordV1 are
// the shapes written by the release before extra quota limits existed. They
// create a representative existing database that must survive the upgrade.
type subscriptionPlanV1 struct {
	Id                      int     `gorm:"column:id;primaryKey"`
	Title                   string  `gorm:"type:varchar(128);not null"`
	Subtitle                string  `gorm:"type:varchar(255);default:''"`
	PriceAmount             float64 `gorm:"type:decimal(10,6);not null;default:0"`
	Currency                string  `gorm:"type:varchar(8);not null;default:'USD'"`
	DurationUnit            string  `gorm:"type:varchar(16);not null;default:'month'"`
	DurationValue           int     `gorm:"type:int;not null;default:1"`
	CustomSeconds           int64   `gorm:"type:bigint;not null;default:0"`
	Enabled                 bool    `gorm:"default:true"`
	SortOrder               int     `gorm:"type:int;default:0"`
	AllowBalancePay         *bool   `gorm:"column:allow_balance_pay"`
	AllowWalletOverflow     *bool   `gorm:"column:allow_wallet_overflow"`
	StripePriceId           string  `gorm:"type:varchar(128);default:''"`
	CreemProductId          string  `gorm:"type:varchar(128);default:''"`
	WaffoPancakeProductId   string  `gorm:"type:varchar(128);default:''"`
	MaxPurchasePerUser      int     `gorm:"type:int;default:0"`
	UpgradeGroup            string  `gorm:"type:varchar(64);default:''"`
	DowngradeGroup          string  `gorm:"type:varchar(64);default:''"`
	TotalAmount             int64   `gorm:"type:bigint;not null;default:0"`
	QuotaResetPeriod        string  `gorm:"type:varchar(16);default:'never'"`
	QuotaResetCustomSeconds int64   `gorm:"type:bigint;default:0"`
	CreatedAt               int64   `gorm:"bigint"`
	UpdatedAt               int64   `gorm:"bigint"`
}

func (subscriptionPlanV1) TableName() string { return "subscription_plans" }

type userSubscriptionV1 struct {
	Id                  int    `gorm:"column:id;primaryKey"`
	UserId              int    `gorm:"index"`
	PlanId              int    `gorm:"index"`
	AmountTotal         int64  `gorm:"type:bigint;not null;default:0"`
	AmountUsed          int64  `gorm:"type:bigint;not null;default:0"`
	StartTime           int64  `gorm:"bigint"`
	EndTime             int64  `gorm:"bigint;index"`
	Status              string `gorm:"type:varchar(32);index"`
	Source              string `gorm:"type:varchar(32);default:'order'"`
	LastResetTime       int64  `gorm:"type:bigint;default:0"`
	NextResetTime       int64  `gorm:"type:bigint;default:0;index"`
	UpgradeGroup        string `gorm:"type:varchar(64);default:''"`
	PrevUserGroup       string `gorm:"type:varchar(64);default:''"`
	DowngradeGroup      string `gorm:"type:varchar(64);default:''"`
	AllowWalletOverflow bool   `gorm:"column:allow_wallet_overflow"`
	CreatedAt           int64  `gorm:"bigint"`
	UpdatedAt           int64  `gorm:"bigint"`
}

func (userSubscriptionV1) TableName() string { return "user_subscriptions" }

type subscriptionPreConsumeRecordV1 struct {
	Id                 int    `gorm:"column:id;primaryKey"`
	RequestId          string `gorm:"type:varchar(64);uniqueIndex"`
	UserId             int    `gorm:"index"`
	UserSubscriptionId int    `gorm:"index"`
	PreConsumed        int64  `gorm:"type:bigint;not null;default:0"`
	Status             string `gorm:"type:varchar(32);index"`
	CreatedAt          int64  `gorm:"bigint"`
	UpdatedAt          int64  `gorm:"bigint;index"`
}

func (subscriptionPreConsumeRecordV1) TableName() string {
	return "subscription_pre_consume_records"
}

// runSubscriptionLimitMigration opens the dialect's database, creates the
// pre-feature schema with data in it, then runs the real startup migration
// twice and proves the new columns exist, are idempotent and keep the old rows.
func runSubscriptionLimitMigration(t *testing.T, db *gorm.DB, dialect common.DatabaseType) {
	t.Helper()

	var version string
	versionQuery := "SELECT version()"
	if dialect == common.DatabaseTypeSQLite {
		versionQuery = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("%s version: %s", dialect, version)

	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(dialect, dialect)
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
	})

	// ---- database as written by the previous release ----
	// An external MySQL/PostgreSQL server keeps its data between runs, so start
	// from the tables the previous release would have created.
	require.NoError(t, db.Migrator().DropTable(&subscriptionPreConsumeRecordV1{}, &userSubscriptionV1{}, &subscriptionPlanV1{}))
	require.NoError(t, db.AutoMigrate(&subscriptionPlanV1{}, &userSubscriptionV1{}, &subscriptionPreConsumeRecordV1{}))
	allowBalance := true
	require.NoError(t, db.Create(&subscriptionPlanV1{
		Id: 7701, Title: "Legacy plan", PriceAmount: 9.5, DurationUnit: SubscriptionDurationMonth,
		DurationValue: 1, Enabled: true, TotalAmount: 5000, QuotaResetPeriod: SubscriptionResetDaily,
		AllowBalancePay: &allowBalance, AllowWalletOverflow: &allowBalance,
	}).Error)
	require.NoError(t, db.Create(&userSubscriptionV1{
		Id: 7702, UserId: 771, PlanId: 7701, AmountTotal: 5000, AmountUsed: 1234,
		StartTime: 1700000000, EndTime: 1900000000, Status: "active", Source: "order",
		NextResetTime: 1700086400,
	}).Error)
	require.NoError(t, db.Create(&subscriptionPreConsumeRecordV1{
		Id: 7703, RequestId: "legacy-request", UserId: 771, UserSubscriptionId: 7702,
		PreConsumed: 25, Status: "consumed",
	}).Error)

	// ---- startup migration, run twice to prove idempotency ----
	firstRun := true
	migrate := func() {
		recorder := &migrationSQLRecorder{}
		recording := db.Session(&gorm.Session{Logger: recorder})
		savedDB := DB
		DB = recording
		defer func() { DB = savedDB }()
		require.NoError(t, recording.AutoMigrate(&UserSubscription{}, &SubscriptionPreConsumeRecord{}))
		if dialect == common.DatabaseTypeSQLite {
			require.NoError(t, ensureSubscriptionPlanTableSQLite())
		} else {
			require.NoError(t, recording.AutoMigrate(&SubscriptionPlan{}))
		}
		if !firstRun {
			// MySQL re-issues MODIFY COLUMN for the pre-existing decimal
			// price_amount on every AutoMigrate (known driver normalization
			// quirk, present before this feature). The new columns must not
			// be touched on a repeated startup on any dialect.
			for _, statement := range recorder.schemaMutations() {
				if dialect == common.DatabaseTypeMySQL &&
					strings.Contains(statement, "price_amount") {
					continue
				}
				assert.Fail(t, "repeated startup must not change the schema", statement)
			}
		}
	}
	migrate()
	firstRun = false
	migrate()

	// ---- the new columns exist and hold JSON, the old data is intact ----
	assert.True(t, db.Migrator().HasColumn(&SubscriptionPlan{}, "quota_limits"))
	assert.True(t, db.Migrator().HasColumn(&UserSubscription{}, "quota_limit_states"))
	assert.True(t, db.Migrator().HasColumn(&SubscriptionPreConsumeRecord{}, "limit_refs"))

	var legacyPlan SubscriptionPlan
	require.NoError(t, db.Where("id = ?", 7701).First(&legacyPlan).Error)
	assert.Equal(t, "Legacy plan", legacyPlan.Title)
	assert.EqualValues(t, 5000, legacyPlan.TotalAmount)
	assert.Equal(t, SubscriptionResetDaily, legacyPlan.QuotaResetPeriod)
	assert.Nil(t, legacyPlan.QuotaLimits, "an upgraded row must read as having no limits")

	var legacySub UserSubscription
	require.NoError(t, db.Where("id = ?", 7702).First(&legacySub).Error)
	assert.EqualValues(t, 1234, legacySub.AmountUsed)
	assert.Nil(t, legacySub.QuotaLimitStates)

	var legacyRecord SubscriptionPreConsumeRecord
	require.NoError(t, db.Where("request_id = ?", "legacy-request").First(&legacyRecord).Error)
	assert.EqualValues(t, 25, legacyRecord.PreConsumed)
	assert.Nil(t, legacyRecord.LimitRefs)

	// ---- writing and reading the new columns through the real driver ----
	limits, err := ValidateSubscriptionQuotaLimits([]SubscriptionQuotaLimit{
		{Name: "5h", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100},
		{Name: "7d", WindowSeconds: SubscriptionLimitWindow7d, AmountTotal: 1000},
		{Name: "模型独立", AmountTotal: 50, Models: []string{"claude-*"}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&SubscriptionPlan{}).Where("id = ?", 7701).
		Update("quota_limits", SubscriptionQuotaLimits(limits)).Error)
	require.NoError(t, db.Model(&UserSubscription{}).Where("id = ?", 7702).
		Update("quota_limit_states", SubscriptionQuotaLimitStates{
			{Key: "limit-1", Used: 40, WindowFrom: 1700000000},
			{Key: "limit-3", Model: "claude-sonnet-4", Used: 10},
		}).Error)
	require.NoError(t, db.Model(&SubscriptionPreConsumeRecord{}).Where("id = ?", 7703).
		Update("limit_refs", SubscriptionLimitRefs{
			{Key: "limit-1"}, {Key: "limit-3", Model: "claude-sonnet-4"},
		}).Error)

	var reloadedPlan SubscriptionPlan
	require.NoError(t, db.Where("id = ?", 7701).First(&reloadedPlan).Error)
	require.Len(t, reloadedPlan.QuotaLimits, 3)
	assert.EqualValues(t, SubscriptionLimitWindow5h, reloadedPlan.QuotaLimits[0].WindowSeconds)
	assert.EqualValues(t, SubscriptionLimitWindow7d, reloadedPlan.QuotaLimits[1].WindowSeconds)
	assert.Equal(t, []string{"claude-*"}, reloadedPlan.QuotaLimits[2].Models)
	assert.Equal(t, "模型独立", reloadedPlan.QuotaLimits[2].Name)

	var reloadedSub UserSubscription
	require.NoError(t, db.Where("id = ?", 7702).First(&reloadedSub).Error)
	require.Len(t, reloadedSub.QuotaLimitStates, 2)
	assert.EqualValues(t, 40, reloadedSub.QuotaLimitStates[0].Used)
	assert.Equal(t, "claude-sonnet-4", reloadedSub.QuotaLimitStates[1].Model)

	var reloadedRecord SubscriptionPreConsumeRecord
	require.NoError(t, db.Where("id = ?", 7703).First(&reloadedRecord).Error)
	require.Len(t, reloadedRecord.LimitRefs, 2)
	assert.Equal(t, "limit-3", reloadedRecord.LimitRefs[1].Key)

	// ---- NULL must keep decoding as "no limits" ----
	require.NoError(t, db.Exec("UPDATE subscription_plans SET quota_limits = NULL WHERE id = ?", 7701).Error)
	require.NoError(t, db.Exec("UPDATE user_subscriptions SET quota_limit_states = NULL WHERE id = ?", 7702).Error)
	var nulledPlan SubscriptionPlan
	require.NoError(t, db.Where("id = ?", 7701).First(&nulledPlan).Error)
	assert.Nil(t, nulledPlan.QuotaLimits)
	var nulledSub UserSubscription
	require.NoError(t, db.Where("id = ?", 7702).First(&nulledSub).Error)
	assert.Nil(t, nulledSub.QuotaLimitStates)

	// ---- removing every limit through the admin update path writes NULL ----
	// AdminUpdateSubscriptionPlan passes the normalized (nil) slice through an
	// Updates map, so the Valuer must yield NULL rather than an empty string.
	require.NoError(t, db.Model(&SubscriptionPlan{}).Where("id = ?", 7701).
		Updates(map[string]any{"quota_limits": SubscriptionQuotaLimits(nil)}).Error)
	var cleared SubscriptionPlan
	require.NoError(t, db.Where("id = ?", 7701).First(&cleared).Error)
	assert.Nil(t, cleared.QuotaLimits)
	assert.Nil(t, cleared.GetQuotaLimits())
	var storedValue *string
	require.NoError(t, db.Raw("SELECT quota_limits FROM subscription_plans WHERE id = ?", 7701).Scan(&storedValue).Error)
	assert.Nil(t, storedValue, "cleared limits must persist as NULL, not an empty string")

	// ---- a fresh database gets the columns directly ----
	fresh := openSubscriptionLimitTestDB(t, dialect)
	require.NoError(t, fresh.Migrator().DropTable("subscription_plans_fresh", "user_subscriptions_fresh", "subscription_pre_consume_records_fresh"))
	require.NoError(t, fresh.AutoMigrate(&SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}))
	assert.True(t, fresh.Migrator().HasColumn(&SubscriptionPlan{}, "quota_limits"))
	assert.True(t, fresh.Migrator().HasColumn(&UserSubscription{}, "quota_limit_states"))
	assert.True(t, fresh.Migrator().HasColumn(&SubscriptionPreConsumeRecord{}, "limit_refs"))
}

func openSubscriptionLimitTestDB(t *testing.T, dialect common.DatabaseType) *gorm.DB {
	t.Helper()
	switch dialect {
	case common.DatabaseTypeSQLite:
		path := common.SQLitePath
		if path == "" {
			path = filepath.Join(t.TempDir(), "fresh.db")
		}
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		require.NoError(t, err)
		return db
	case common.DatabaseTypeMySQL:
		dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
		require.NotEmpty(t, dsn, "TEST_MYSQL_DSN is not configured")
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		return db
	default:
		dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
		require.NotEmpty(t, dsn, "TEST_POSTGRES_DSN is not configured")
		db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
		require.NoError(t, err)
		return db
	}
}

func TestSubscriptionQuotaLimitMigrationSQLite(t *testing.T) {
	previousPath := common.SQLitePath
	common.SQLitePath = filepath.Join(t.TempDir(), "legacy.db")
	t.Cleanup(func() { common.SQLitePath = previousPath })

	db, err := gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	runSubscriptionLimitMigration(t, db, common.DatabaseTypeSQLite)
}

func TestSubscriptionQuotaLimitMigrationMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}
	db := openSubscriptionLimitTestDB(t, common.DatabaseTypeMySQL)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	runSubscriptionLimitMigration(t, db, common.DatabaseTypeMySQL)
}

func TestSubscriptionQuotaLimitMigrationPostgreSQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	db := openSubscriptionLimitTestDB(t, common.DatabaseTypePostgreSQL)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	runSubscriptionLimitMigration(t, db, common.DatabaseTypePostgreSQL)
}

// runSubscriptionLimitBillingFlow exercises the full pre-consume → settle →
// refund chain against a real database so the text/JSON columns and the row
// locking behave the same on every supported engine.
func runSubscriptionLimitBillingFlow(t *testing.T, dialect common.DatabaseType) {
	t.Helper()
	db := openSubscriptionLimitTestDB(t, dialect)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	if dialect != common.DatabaseTypeSQLite {
		sqlDB.SetMaxOpenConns(1)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.Migrator().DropTable(
		"subscription_pre_consume_records", "user_subscriptions", "subscription_plans"))
	require.NoError(t, db.AutoMigrate(&SubscriptionPlan{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}))

	oldDB, oldLogDB := DB, LOG_DB
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(dialect, dialect)
	purgeSubscriptionPlanCache(t)
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.SetDatabaseTypes(oldMainType, oldLogType)
		purgeSubscriptionPlanCache(t)
	})

	limits, err := ValidateSubscriptionQuotaLimits([]SubscriptionQuotaLimit{
		{Name: "5h", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100},
		{Name: "7d", WindowSeconds: SubscriptionLimitWindow7d, AmountTotal: 1000},
		{Name: "模型独立", AmountTotal: 50, Models: []string{"claude-*"}},
	})
	require.NoError(t, err)
	plan := &SubscriptionPlan{
		Title: "Matrix", DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount: 0, QuotaResetPeriod: SubscriptionResetNever, QuotaLimits: limits,
	}
	require.NoError(t, db.Create(plan).Error)
	now := GetDBTimestamp()
	sub := &UserSubscription{
		UserId: 501, PlanId: plan.Id, StartTime: now - 60, EndTime: now + 86400, Status: "active",
	}
	require.NoError(t, db.Create(sub).Error)

	// Pre-consume charges every matching counter through real SQL.
	res, err := PreConsumeUserSubscription("matrix-req-1", 501, "claude-sonnet-4", 0, 20)
	require.NoError(t, err)
	require.Len(t, res.LimitRefs, 3)

	var reloaded UserSubscription
	require.NoError(t, db.Where("id = ?", sub.Id).First(&reloaded).Error)
	h5, index := findSubscriptionLimitState(reloaded.QuotaLimitStates, "limit-1", "")
	require.True(t, index >= 0)
	assert.EqualValues(t, 20, h5.Used)
	claude, index := findSubscriptionLimitState(reloaded.QuotaLimitStates, "limit-3", "claude-sonnet-4")
	require.True(t, index >= 0)
	assert.EqualValues(t, 20, claude.Used)

	// The per-model cap of 50 blocks a second 40-unit request.
	_, err = PreConsumeUserSubscription("matrix-req-2", 501, "claude-sonnet-4", 0, 40)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscription quota insufficient")

	// A refund releases the reserved counters back to zero.
	require.NoError(t, RefundSubscriptionPreConsume("matrix-req-1"))
	require.NoError(t, RefundSubscriptionPreConsume("matrix-req-1"))
	require.NoError(t, db.Where("id = ?", sub.Id).First(&reloaded).Error)
	assert.EqualValues(t, 0, reloaded.AmountUsed)
	h5, _ = findSubscriptionLimitState(reloaded.QuotaLimitStates, "limit-1", "")
	assert.EqualValues(t, 0, h5.Used)
	claude, _ = findSubscriptionLimitState(reloaded.QuotaLimitStates, "limit-3", "claude-sonnet-4")
	assert.EqualValues(t, 0, claude.Used)

	// A second request with the same model settles to its real cost.
	res, err = PreConsumeUserSubscription("matrix-req-3", 501, "claude-sonnet-4", 0, 20)
	require.NoError(t, err)
	require.NoError(t, PostConsumeUserSubscriptionDeltaWithLimits(sub.Id, 10, res.LimitRefs))
	require.NoError(t, db.Where("id = ?", sub.Id).First(&reloaded).Error)
	assert.EqualValues(t, 30, reloaded.AmountUsed)
	h5, _ = findSubscriptionLimitState(reloaded.QuotaLimitStates, "limit-1", "")
	assert.EqualValues(t, 30, h5.Used)
	claude, _ = findSubscriptionLimitState(reloaded.QuotaLimitStates, "limit-3", "claude-sonnet-4")
	assert.EqualValues(t, 30, claude.Used)
}

func TestSubscriptionQuotaLimitBillingFlowSQLite(t *testing.T) {
	previousPath := common.SQLitePath
	common.SQLitePath = filepath.Join(t.TempDir(), "billing.db")
	t.Cleanup(func() { common.SQLitePath = previousPath })
	runSubscriptionLimitBillingFlow(t, common.DatabaseTypeSQLite)
}

func TestSubscriptionQuotaLimitBillingFlowMySQL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("TEST_MYSQL_DSN")) == "" {
		t.Skip("TEST_MYSQL_DSN is not configured")
	}
	runSubscriptionLimitBillingFlow(t, common.DatabaseTypeMySQL)
}

func TestSubscriptionQuotaLimitBillingFlowPostgreSQL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN")) == "" {
		t.Skip("TEST_POSTGRES_DSN is not configured")
	}
	runSubscriptionLimitBillingFlow(t, common.DatabaseTypePostgreSQL)
}
