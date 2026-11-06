package model

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedBatchPlan creates an enabled monthly plan an administrator can grant.
func seedBatchPlan(t *testing.T, id int, title string, upgradeGroup string) *SubscriptionPlan {
	t.Helper()
	purgeSubscriptionPlanCache(t)
	plan := &SubscriptionPlan{
		Id: id, Title: title, PriceAmount: 0,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount: 1000, QuotaResetPeriod: SubscriptionResetNever,
		UpgradeGroup: upgradeGroup,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

func seedBatchUser(t *testing.T, id int, group string) int {
	t.Helper()
	user := &User{
		Id: id, Username: fmt.Sprintf("batch-user-%d", id),
		AffCode: fmt.Sprintf("aff-batch-%d", id),
		Quota:   500, Status: common.UserStatusEnabled, Group: group,
	}
	require.NoError(t, DB.Create(user).Error)
	return user.Id
}

// A batch grant must create one subscription per requested user, report each
// outcome separately, and keep the per-user purchase cap working: a user who is
// already at the cap fails alone while the others still succeed.
func TestBatchGrantSubscriptionToUsers(t *testing.T) {
	truncateTables(t)

	const planId = 9401
	plan := seedBatchPlan(t, planId, "Grantable", "")
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", planId).
		Update("max_purchase_per_user", 1).Error)

	userIdA := seedBatchUser(t, 401, "default")
	userIdB := seedBatchUser(t, 402, "default")

	result, err := BatchGrantSubscriptionToUsers([]int{userIdA, userIdB}, planId)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Requested)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 0, result.Failed)
	for _, item := range result.Items {
		assert.True(t, item.Ok, "item %+v", item)
	}

	// Granting a second time hits the per-user cap for both entries, and neither
	// failure hides the other.
	result, err = BatchGrantSubscriptionToUsers([]int{userIdA, userIdB}, planId)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Succeeded)
	assert.Equal(t, 2, result.Failed)
	for _, item := range result.Items {
		assert.False(t, item.Ok)
		assert.NotEmpty(t, item.Message)
	}

	// One missing user must not roll back the successful entry beside it: a
	// fresh user in the same batch still gets their subscription.
	userIdC := seedBatchUser(t, 403, "default")
	result, err = BatchGrantSubscriptionToUsers([]int{404, userIdC}, planId)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 1, result.Failed)
	assert.False(t, result.Items[0].Ok, "a user that does not exist must fail")
	assert.True(t, result.Items[1].Ok)

	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("plan_id = ?", planId).Count(&count).Error)
	assert.EqualValues(t, 3, count, "every successful grant persists exactly once")
	require.NotNil(t, plan)
}

// Batch invalidate must cancel every subscription it is given, end it now, and
// report each entry. A subscription that is already cancelled is not an error:
// the operator asked for an end state that already holds.
func TestBatchInvalidateUserSubscriptions(t *testing.T) {
	truncateTables(t)

	plan := seedBatchPlan(t, 9402, "Invalidatable", "")
	subA := seedQuotaLimitSubscription(t, 410, plan.Id, plan.TotalAmount)
	subB := seedQuotaLimitSubscription(t, 411, plan.Id, plan.TotalAmount)

	result, err := BatchInvalidateUserSubscriptions([]int{subA, subB, 999999})
	require.NoError(t, err)
	assert.Equal(t, 3, result.Requested)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 1, result.Failed)
	assert.False(t, result.Items[2].Ok, "a subscription that does not exist must fail")

	for _, subId := range []int{subA, subB} {
		var sub UserSubscription
		require.NoError(t, DB.Where("id = ?", subId).First(&sub).Error)
		assert.Equal(t, "cancelled", sub.Status)
		assert.LessOrEqual(t, sub.EndTime, GetDBTimestamp())
	}

	// Invalidating again is a no-op success: the row exists and is already ended.
	result, err = BatchInvalidateUserSubscriptions([]int{subA})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Succeeded)
}

// Batch delete must remove the rows it is given and leave the rest of the table
// untouched.
func TestBatchDeleteUserSubscriptions(t *testing.T) {
	truncateTables(t)

	plan := seedBatchPlan(t, 9403, "Deletable", "")
	subA := seedQuotaLimitSubscription(t, 420, plan.Id, plan.TotalAmount)
	subB := seedQuotaLimitSubscription(t, 421, plan.Id, plan.TotalAmount)
	keep := seedQuotaLimitSubscription(t, 422, plan.Id, plan.TotalAmount)

	result, err := BatchDeleteUserSubscriptions([]int{subA, subB})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 0, result.Failed)

	var remaining []UserSubscription
	require.NoError(t, DB.Find(&remaining).Error)
	require.Len(t, remaining, 1)
	assert.Equal(t, keep, remaining[0].Id)
}

// A batch reset zeroes the usage counters of every subscription it names, and it
// must clear the per-limit counters too — including a running 5h window only when
// the plan's reset cycle says so, exactly like the single-subscription reset.
func TestBatchResetUserSubscriptions(t *testing.T) {
	truncateTables(t)

	plan := seedBatchPlan(t, 9404, "Resettable", "")
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]any{
		"quota_reset_period": SubscriptionResetDaily,
		"quota_limits": SubscriptionQuotaLimits{
			{Key: "period", Name: "Period", AmountTotal: 1000},
			{Key: "h5", Name: "5h", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100},
			{Key: "d7", Name: "7d", WindowSeconds: SubscriptionLimitWindow7d, AmountTotal: 500},
		},
	}).Error)
	purgeSubscriptionPlanCache(t)

	now := GetDBTimestamp()
	subA := seedQuotaLimitSubscription(t, 430, plan.Id, plan.TotalAmount)
	subB := seedQuotaLimitSubscription(t, 431, plan.Id, plan.TotalAmount)
	// Seed counters that a daily reset clears, and a window that is still running.
	seedStates := SubscriptionQuotaLimitStates{
		{Key: "period", Used: 120},
		{Key: "h5", Used: 40, WindowFrom: now},
		{Key: "d7", Used: 90, WindowFrom: now},
	}
	for _, subId := range []int{subA, subB} {
		require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subId).Updates(map[string]any{
			"amount_used":        120,
			"quota_limit_states": seedStates,
			"next_reset_time":    now + 3600,
		}).Error)
	}

	result, err := BatchResetUserSubscriptions([]int{subA, subB}, false)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 0, result.Failed)

	for _, subId := range []int{subA, subB} {
		sub := loadQuotaLimitSubscription(t, subId)
		assert.EqualValues(t, 0, sub.AmountUsed, "whole-period usage must be cleared")
		// A daily reset governs window-less counters; a running 5h window keeps its
		// timer so the operator cannot shorten what the user already earned.
		h5, index := findSubscriptionLimitState(sub.QuotaLimitStates, "h5", "")
		assert.GreaterOrEqual(t, index, 0)
		assert.EqualValues(t, 40, h5.Used, "a running window counter keeps its usage")
		totalCap, index := findSubscriptionLimitState(sub.QuotaLimitStates, "period", "")
		assert.GreaterOrEqual(t, index, 0)
		assert.EqualValues(t, 0, totalCap.Used, "a reset-cycle counter must be zeroed")
	}
}

// Batch input bounds: an empty list and an oversized list are both refused before
// any row is touched.
func TestSubscriptionBatchInputBounds(t *testing.T) {
	truncateTables(t)

	plan := seedBatchPlan(t, 9405, "Bounded", "")
	subId := seedQuotaLimitSubscription(t, 440, plan.Id, plan.TotalAmount)

	_, err := BatchInvalidateUserSubscriptions(nil)
	assert.ErrorIs(t, err, ErrSubscriptionBatchEmpty)
	_, err = BatchDeleteUserSubscriptions([]int{})
	assert.ErrorIs(t, err, ErrSubscriptionBatchEmpty)
	_, err = BatchResetUserSubscriptions(nil, false)
	assert.ErrorIs(t, err, ErrSubscriptionBatchEmpty)
	_, err = BatchGrantSubscriptionToUsers(nil, plan.Id)
	assert.ErrorIs(t, err, ErrSubscriptionBatchEmpty)

	oversized := make([]int, subscriptionBatchMax+1)
	for i := range oversized {
		oversized[i] = subId
	}
	_, err = BatchInvalidateUserSubscriptions(oversized)
	assert.ErrorIs(t, err, ErrSubscriptionBatchTooLarge)

	_, err = BatchGrantSubscriptionToUsers([]int{441}, 0)
	assert.Error(t, err, "a non-positive plan id must be refused")

	// The refused calls left the subscription alone.
	var sub UserSubscription
	require.NoError(t, DB.Where("id = ?", subId).First(&sub).Error)
	assert.Equal(t, "active", sub.Status)
}

// The cross-user list is what makes bulk management possible: it returns rows
// from many users on one page, resolves the owner's username and the plan title
// so the table needs no per-row lookup, and applies the filters an administrator
// narrows the selection with.
func TestListUserSubscriptionsAcrossUsers(t *testing.T) {
	truncateTables(t)

	planA := seedBatchPlan(t, 9406, "Plan A", "")
	planB := seedBatchPlan(t, 9407, "Plan B", "")
	purgeSubscriptionPlanCache(t)

	userA := seedBatchUser(t, 450, "default")
	userB := seedBatchUser(t, 451, "vip")

	subA := seedQuotaLimitSubscription(t, userA, planA.Id, planA.TotalAmount)
	seedQuotaLimitSubscription(t, userB, planA.Id, planA.TotalAmount)
	subC := seedQuotaLimitSubscription(t, userB, planB.Id, planB.TotalAmount)
	require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", subC).
		Update("status", "cancelled").Error)

	page := &common.PageInfo{Page: 1, PageSize: 10}

	items, total, err := ListUserSubscriptions(UserSubscriptionListFilter{}, page)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, items, 3)
	// Newest first, so the row just cancelled is on top.
	assert.Equal(t, subC, items[0].Subscription.Id)
	assert.Equal(t, "Plan B", items[0].PlanTitle)
	assert.Equal(t, fmt.Sprintf("batch-user-%d", userB), items[0].Username)

	// Status filter.
	items, total, err = ListUserSubscriptions(UserSubscriptionListFilter{Status: "active"}, page)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	for _, item := range items {
		assert.Equal(t, "active", item.Subscription.Status)
	}

	// Plan filter.
	items, total, err = ListUserSubscriptions(UserSubscriptionListFilter{PlanId: planB.Id}, page)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, subC, items[0].Subscription.Id)

	// User filter.
	items, total, err = ListUserSubscriptions(UserSubscriptionListFilter{UserId: userA}, page)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, subA, items[0].Subscription.Id)

	// Keyword matches a username and a literal user id.
	for _, keyword := range []string{fmt.Sprintf("batch-user-%d", userB), fmt.Sprint(userB)} {
		items, total, err = ListUserSubscriptions(UserSubscriptionListFilter{Keyword: keyword}, page)
		require.NoError(t, err)
		assert.EqualValues(t, 2, total, "keyword %q", keyword)
		for _, item := range items {
			assert.Equal(t, userB, item.UserId)
		}
	}

	// A keyword that matches nobody returns an empty page, not an error.
	items, total, err = ListUserSubscriptions(UserSubscriptionListFilter{Keyword: "no-such-user"}, page)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
	assert.Empty(t, items)

	// ActiveOnly drops the cancelled row and would drop an expired one too.
	items, total, err = ListUserSubscriptions(UserSubscriptionListFilter{ActiveOnly: true}, page)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)

	// Pagination reports the full count while returning one page.
	page = &common.PageInfo{Page: 2, PageSize: 2}
	items, total, err = ListUserSubscriptions(UserSubscriptionListFilter{}, page)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	require.Len(t, items, 1)

	_, _, err = ListUserSubscriptions(UserSubscriptionListFilter{}, nil)
	assert.Error(t, err, "a missing page descriptor must be refused")
}

// A plan's whitelist is stored and read back like the other JSON columns, so an
// administrator's grant cannot silently widen a plan that was scoped to a few
// models.
func TestSubscriptionAllowedModelsRoundTrip(t *testing.T) {
	truncateTables(t)

	plan := seedBatchPlan(t, 9408, "Scoped Plan", "")
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Update("allowed_models", SubscriptionModelList{"gpt-5.5", "claude-*"}).Error)
	purgeSubscriptionPlanCache(t)

	var reloaded SubscriptionPlan
	require.NoError(t, DB.Where("id = ?", plan.Id).First(&reloaded).Error)
	assert.Equal(t, []string{"gpt-5.5", "claude-*"}, reloaded.GetAllowedModels())
	assert.True(t, reloaded.AllowsModel("gpt-5.5"))
	assert.True(t, reloaded.AllowsModel("claude-sonnet-4"))
	assert.False(t, reloaded.AllowsModel("gemini-2.0"))

	// Clearing the whitelist restores "every model".
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Update("allowed_models", SubscriptionModelList(nil)).Error)
	purgeSubscriptionPlanCache(t)
	var cleared SubscriptionPlan
	require.NoError(t, DB.Where("id = ?", plan.Id).First(&cleared).Error)
	assert.Nil(t, cleared.AllowedModels)
	assert.True(t, cleared.AllowsModel("gemini-2.0"))
}

// The list endpoints must show which models a subscription pays for, so an
// administrator can see a scoped grant without opening the plan.
func TestSubscriptionSummariesCarryAllowedModels(t *testing.T) {
	truncateTables(t)

	plan := seedBatchPlan(t, 9410, "Scoped summary", "")
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).
		Update("allowed_models", SubscriptionModelList{"gpt-5.5"}).Error)
	purgeSubscriptionPlanCache(t)

	openPlan := seedBatchPlan(t, 9411, "Open summary", "")

	scopedUser := seedBatchUser(t, 470, "default")
	openUser := seedBatchUser(t, 471, "default")
	seedQuotaLimitSubscription(t, scopedUser, plan.Id, plan.TotalAmount)
	seedQuotaLimitSubscription(t, openUser, openPlan.Id, openPlan.TotalAmount)

	summaries, err := GetAllActiveUserSubscriptions(scopedUser)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, []string{"gpt-5.5"}, summaries[0].AllowedModels)

	// A plan without a whitelist omits the field rather than sending an empty
	// list, so clients can treat presence as "this subscription is scoped".
	summaries, err = GetAllActiveUserSubscriptions(openUser)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Nil(t, summaries[0].AllowedModels)

	items, _, err := ListUserSubscriptions(UserSubscriptionListFilter{UserId: scopedUser}, &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, []string{"gpt-5.5"}, items[0].AllowedModels)
}

// Reading the database clock from inside an open transaction must use the
// transaction's own connection. On a single-connection pool — which is what the
// SQLite test harness uses, and what an operator gets by setting
// SQL_MAX_OPEN_CONNS=1 — a clock query through the shared handle waits for a
// connection the transaction will never release, deadlocking the caller.
//
// CreateUserSubscriptionFromPlanTx used to do exactly that, so every admin grant
// and every paid subscription completion could hang. This guards the invariant:
// it must return quickly, not block.
func TestGetDBTimestampTxDoesNotDeadlockInsideTransaction(t *testing.T) {
	truncateTables(t)

	sqlDB, err := DB.DB()
	require.NoError(t, err)
	// One connection makes any cross-handle query inside a transaction block,
	// which is the condition the deadlock needs. The harness may already run on
	// a single connection, so restore whatever limit was in effect instead of
	// assuming one: leaving a wider pool behind would change how every later
	// test in this package serializes.
	previousLimit := sqlDB.Stats().MaxOpenConnections
	sqlDB.SetMaxOpenConns(1)
	// SetMaxOpenConns(0) means unlimited, which is exactly what a previousLimit
	// of 0 says the pool was, so restoring unconditionally is correct.
	t.Cleanup(func() { sqlDB.SetMaxOpenConns(previousLimit) })

	plan := seedBatchPlan(t, 9409, "Clock-safe plan", "")
	userId := seedBatchUser(t, 460, "default")

	// The real path that regressed: granting a plan opens a transaction, locks
	// the user row, then needs the clock to stamp start/end times.
	done := make(chan error, 1)
	go func() {
		_, err := AdminBindSubscription(userId, plan.Id, "")
		done <- err
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("granting a subscription blocked inside its transaction: " +
			"the clock read must use the transaction handle, not the shared pool")
	}

	// And the primitive itself must keep working on the shared handle too.
	var now int64
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		now = GetDBTimestampTx(tx)
		if now <= 0 {
			return errors.New("clock read returned a non-positive timestamp")
		}
		return nil
	}))
	assert.NotZero(t, now)
	assert.NotZero(t, GetDBTimestamp(), "the nil-tx form still reads database time")
}
