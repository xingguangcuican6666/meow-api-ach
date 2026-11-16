package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupResetCardTestDatabase repoints the package database globals at a real
// server when TEST_RESET_CARD_DIALECT selects mysql or postgres, so the reset
// card scenarios below run against every supported engine instead of only the
// in-memory SQLite opened by TestMain.
func setupResetCardTestDatabase(t *testing.T) {
	t.Helper()
	dialect := strings.TrimSpace(os.Getenv("TEST_RESET_CARD_DIALECT"))
	if dialect == "" || dialect == "sqlite" {
		return
	}
	dsnEnv := "TEST_" + strings.ToUpper(dialect) + "_DSN"
	dsn := strings.TrimSpace(os.Getenv(dsnEnv))
	if dsn == "" {
		t.Skipf("%s is not configured", dsnEnv)
	}
	db, dbType, err := chooseDB(dsnEnv, false)
	require.NoError(t, err)
	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = db, db
	common.SetDatabaseTypes(dbType, dbType)
	initCol()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		initCol()
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(
		&User{}, &Token{}, &Log{}, &SubscriptionPlan{}, &UserSubscription{},
		&SubscriptionPreConsumeRecord{}, &SubscriptionResetCard{},
	))
	// The shared server may still hold rows from another test file that ran
	// against the same DSN; recipient resolution counts every enabled user, so
	// start from an empty slate instead of inheriting someone else's fixtures.
	for _, table := range []string{
		"subscription_reset_cards", "subscription_pre_consume_records",
		"user_subscriptions", "subscription_plans", "logs", "tokens", "users",
	} {
		require.NoError(t, db.Exec("DELETE FROM "+table).Error)
	}
	var version string
	versionQuery := "SELECT version()"
	if dbType == common.DatabaseTypeSQLite {
		versionQuery = "SELECT sqlite_version()"
	}
	require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
	t.Logf("reset card test database: %s %s", dialect, version)
}

// seedResetCardPlan creates a plan whose reset cycle follows a daily boundary.
func seedResetCardPlan(t *testing.T, id int, title string, total int64) *SubscriptionPlan {
	t.Helper()
	purgeSubscriptionPlanCache(t)
	plan := &SubscriptionPlan{
		Id: id, Title: title, PriceAmount: 0,
		DurationUnit: SubscriptionDurationMonth, DurationValue: 1,
		TotalAmount: total, QuotaResetPeriod: SubscriptionResetDaily,
	}
	require.NoError(t, DB.Create(plan).Error)
	return plan
}

// seedResetCardSubscription seeds an active subscription with counters that a
// card can clear: whole-period usage, a cycle-following limit, and a running 5h
// window.
func seedResetCardSubscription(t *testing.T, userId int, plan *SubscriptionPlan, endTime int64) int {
	t.Helper()
	now := GetDBTimestamp()
	sub := &UserSubscription{
		UserId: userId, PlanId: plan.Id,
		AmountTotal: plan.TotalAmount, AmountUsed: 400,
		StartTime: now - 3600, EndTime: endTime, Status: "active",
		LastResetTime: now - 3600, NextResetTime: now + 60,
		QuotaLimitStates: SubscriptionQuotaLimitStates{
			{Key: "period", Used: 120},
			{Key: "h5", Used: 40, WindowFrom: now},
		},
	}
	require.NoError(t, DB.Create(sub).Error)
	return sub.Id
}

func seedResetCardLimits(t *testing.T, plan *SubscriptionPlan) {
	t.Helper()
	require.NoError(t, DB.Model(&SubscriptionPlan{}).Where("id = ?", plan.Id).Updates(map[string]any{
		"quota_limits": SubscriptionQuotaLimits{
			{Key: "period", Name: "Period", AmountTotal: 1000},
			{Key: "h5", Name: "5h", WindowSeconds: SubscriptionLimitWindow5h, AmountTotal: 100},
		},
	}).Error)
	purgeSubscriptionPlanCache(t)
}

func loadResetCard(t *testing.T, id int) SubscriptionResetCard {
	t.Helper()
	var card SubscriptionResetCard
	require.NoError(t, DB.Where("id = ?", id).First(&card).Error)
	return card
}

// A card minted for one user resets exactly what its configuration selects: the
// whole-period total and the cycle-following counter, but not the running 5h
// window unless the operator asked for windows too.
func TestRedeemSubscriptionResetCardAppliesConfiguredScope(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9701, "Card plan", 1000)
	seedResetCardLimits(t, plan)
	userId := seedBatchUser(t, 601, "default")
	subId := seedResetCardSubscription(t, userId, plan, GetDBTimestamp()+30*24*3600)

	result, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{userId},
		Count:   1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:      SubscriptionResetCardScopePlan,
			PlanId:           plan.Id,
			ResetAmountUsed:  true,
			ResetCycleLimits: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded)
	require.Len(t, result.Items[0].CardIds, 1)
	cardId := result.Items[0].CardIds[0]

	useResult, err := RedeemSubscriptionResetCard(userId, cardId, "")
	require.NoError(t, err)
	assert.Equal(t, cardId, useResult.CardId)
	assert.Equal(t, 1, useResult.ResetCount)
	assert.True(t, useResult.ResetAmountUsed)
	assert.False(t, useResult.ResetWindowLimits)

	sub := getSubscriptionResetSub(t, subId)
	assert.Zero(t, sub.AmountUsed)
	period, index := findSubscriptionLimitState(sub.QuotaLimitStates, "period", "")
	require.GreaterOrEqual(t, index, 0)
	assert.Zero(t, period.Used)
	h5, index := findSubscriptionLimitState(sub.QuotaLimitStates, "h5", "")
	require.GreaterOrEqual(t, index, 0)
	assert.EqualValues(t, 40, h5.Used, "a running window is untouched unless the card selects windows")

	card := loadResetCard(t, cardId)
	assert.Equal(t, SubscriptionResetCardStatusUsed, card.Status)
	assert.Equal(t, userId, card.UsedByUserId)
	assert.NotZero(t, card.UsedTime)

	// A second redemption must not touch anything.
	_, err = RedeemSubscriptionResetCard(userId, cardId, "")
	assert.ErrorIs(t, err, ErrSubscriptionResetCardUsed)
}

// A window-scoped card also clears the rolling window and its anchor, so the
// next charge opens a fresh window.
func TestRedeemSubscriptionResetCardCanClearWindows(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9702, "Window card plan", 1000)
	seedResetCardLimits(t, plan)
	userId := seedBatchUser(t, 602, "default")
	subId := seedResetCardSubscription(t, userId, plan, GetDBTimestamp()+30*24*3600)

	result, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{userId},
		Count:   1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:       SubscriptionResetCardScopeAll,
			ResetWindowLimits: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded)

	_, err = RedeemSubscriptionResetCard(userId, 0, result.Items[0].Keys[0])
	require.NoError(t, err)

	sub := getSubscriptionResetSub(t, subId)
	assert.EqualValues(t, 400, sub.AmountUsed, "the card did not select the whole-period total")
	h5, index := findSubscriptionLimitState(sub.QuotaLimitStates, "h5", "")
	require.GreaterOrEqual(t, index, 0)
	assert.Zero(t, h5.Used)
	assert.Zero(t, h5.WindowFrom, "clearing a window also drops its anchor")
}

// A card scoped to one subscription must leave the holder's other subscriptions
// alone, and it must not be redeemable by anyone else.
func TestRedeemSubscriptionResetCardTargetsOneSubscription(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	planA := seedResetCardPlan(t, 9703, "Plan A", 1000)
	planB := seedResetCardPlan(t, 9704, "Plan B", 1000)
	userId := seedBatchUser(t, 603, "default")
	otherUserId := seedBatchUser(t, 604, "default")
	endTime := GetDBTimestamp() + 30*24*3600
	subA := seedResetCardSubscription(t, userId, planA, endTime)
	subB := seedResetCardSubscription(t, userId, planB, endTime)

	result, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{userId},
		Count:   1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:        SubscriptionResetCardScopeSubscription,
			UserSubscriptionId: subA,
			ResetAmountUsed:    true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded)
	cardId := result.Items[0].CardIds[0]

	// Another user cannot redeem it even with the correct id.
	_, err = RedeemSubscriptionResetCard(otherUserId, cardId, "")
	assert.ErrorIs(t, err, ErrSubscriptionResetCardNotFound)
	assert.Equal(t, SubscriptionResetCardStatusUnused, loadResetCard(t, cardId).Status)

	_, err = RedeemSubscriptionResetCard(userId, cardId, "")
	require.NoError(t, err)
	assert.Zero(t, getSubscriptionResetSub(t, subA).AmountUsed)
	assert.EqualValues(t, 400, getSubscriptionResetSub(t, subB).AmountUsed,
		"a subscription-scoped card must not touch the holder's other subscriptions")
}

// A card that finds no matching active subscription fails without being spent:
// the user must not lose a card to a click made when nothing was resettable.
func TestRedeemSubscriptionResetCardWithoutTargetKeepsCardUnused(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9705, "Empty plan", 1000)
	userId := seedBatchUser(t, 605, "default")
	// The only subscription is already expired.
	seedResetCardSubscription(t, userId, plan, GetDBTimestamp()-1)

	result, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{userId},
		Count:   1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopeAll,
			ResetAmountUsed: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded)
	cardId := result.Items[0].CardIds[0]

	_, err = RedeemSubscriptionResetCard(userId, cardId, "")
	assert.ErrorIs(t, err, ErrSubscriptionResetCardNoTarget)
	assert.Equal(t, SubscriptionResetCardStatusUnused, loadResetCard(t, cardId).Status,
		"a failed redemption must roll back the status flip")
}

// An expired card is refused, and disabling it keeps it refused.
func TestRedeemSubscriptionResetCardRejectsExpiredAndDisabled(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9706, "Expiring plan", 1000)
	userId := seedBatchUser(t, 606, "default")
	seedResetCardSubscription(t, userId, plan, GetDBTimestamp()+30*24*3600)

	expired, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds:     []int{userId},
		Count:       1,
		ExpiredTime: GetDBTimestamp() - 60,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopeAll,
			ResetAmountUsed: true,
		},
	})
	require.NoError(t, err)
	_, err = RedeemSubscriptionResetCard(userId, expired.Items[0].CardIds[0], "")
	assert.ErrorIs(t, err, ErrSubscriptionResetCardExpired)

	disabled, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{userId},
		Count:   1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopeAll,
			ResetAmountUsed: true,
		},
	})
	require.NoError(t, err)
	cardId := disabled.Items[0].CardIds[0]
	batch, err := BatchInvalidateSubscriptionResetCards([]int{cardId})
	require.NoError(t, err)
	assert.Equal(t, 1, batch.Succeeded)

	_, err = RedeemSubscriptionResetCard(userId, cardId, "")
	assert.ErrorIs(t, err, ErrSubscriptionResetCardDisabled)
}

// Granting n cards per user mints exactly n independent rows per user, all
// sharing one batch id and one configuration, and each with its own key.
func TestBatchGrantSubscriptionResetCardsMintsIndependentCards(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9707, "Batch plan", 1000)
	userA := seedBatchUser(t, 607, "default")
	userB := seedBatchUser(t, 608, "default")

	result, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{userA, userB, userA, 0},
		Count:   3,
		Name:    "Batch card",
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:      SubscriptionResetCardScopePlan,
			PlanId:           plan.Id,
			ResetCycleLimits: true,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Requested, "duplicate and non-positive ids are dropped")
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 3, result.Count)
	assert.NotEmpty(t, result.BatchId)
	firstBatchId := result.BatchId

	seenKeys := make(map[string]struct{})
	for _, item := range result.Items {
		require.True(t, item.Ok, "item %+v", item)
		assert.Len(t, item.CardIds, 3)
		for i, cardId := range item.CardIds {
			card := loadResetCard(t, cardId)
			assert.Equal(t, result.BatchId, card.BatchId)
			assert.Equal(t, item.UserId, card.UserId)
			assert.Equal(t, "Batch card", card.Name)
			assert.Equal(t, plan.Id, card.PlanId)
			assert.True(t, card.ResetCycleLimits)
			assert.False(t, card.ResetAmountUsed)
			assert.Equal(t, SubscriptionResetCardStatusUnused, card.Status)
			assert.Equal(t, item.Keys[i], card.Key)
			_, duplicate := seenKeys[card.Key]
			assert.False(t, duplicate, "every card has its own key")
			seenKeys[card.Key] = struct{}{}
		}
	}

	// A user that does not exist fails alone, leaving the other entry intact.
	result, err = BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{999999, userB},
		Count:   1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopeAll,
			ResetAmountUsed: true,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 1, result.Failed)
	assert.False(t, result.Items[0].Ok)

	cards, err := ListUserSubscriptionResetCards(userA)
	require.NoError(t, err)
	assert.Len(t, cards, 3)

	// Each grant call is its own batch, so filtering by the second batch id
	// returns only the card that call minted.
	assert.NotEqual(t, firstBatchId, result.BatchId, "each grant gets its own batch id")
	items, total, err := ListSubscriptionResetCards(SubscriptionResetCardListFilter{BatchId: result.BatchId}, &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	assert.Equal(t, userB, items[0].Card.UserId)

	// The first batch is untouched by the second grant.
	items, total, err = ListSubscriptionResetCards(SubscriptionResetCardListFilter{BatchId: firstBatchId}, &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(6), total)
	assert.Len(t, items, 6)
}

// A configuration that can never do anything is refused before any row exists.
func TestBatchGrantSubscriptionResetCardsValidatesConfig(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	userId := seedBatchUser(t, 609, "default")

	_, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{userId},
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopeAll,
			ResetAmountUsed: false,
		},
	})
	assert.ErrorIs(t, err, ErrSubscriptionResetScopeEmpty)

	_, err = BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{userId},
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopePlan,
			ResetAmountUsed: true,
		},
	})
	assert.Error(t, err, "a plan-scoped card needs a plan")

	_, err = BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: []int{userId},
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     "nonsense",
			ResetAmountUsed: true,
		},
	})
	assert.Error(t, err)

	// Bounds.
	_, err = BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		UserIds: nil,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopeAll,
			ResetAmountUsed: true,
		},
	})
	assert.ErrorIs(t, err, ErrSubscriptionBatchEmpty)

	var count int64
	require.NoError(t, DB.Model(&SubscriptionResetCard{}).Count(&count).Error)
	assert.Zero(t, count, "a refused grant writes nothing")
}

// A filter grant resolves its recipients server-side: the operator names
// conditions, not people. Deleted and disabled users must never be picked up —
// a card handed to an account that cannot log in is a card wasted.
func TestBatchGrantSubscriptionResetCardsResolvesFilterRecipients(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9710, "Filter plan", 1000)
	vipA := seedBatchUser(t, 620, "vip")
	vipB := seedBatchUser(t, 621, "vip")
	seedBatchUser(t, 622, "default")
	disabledVip := seedBatchUser(t, 623, "vip")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", disabledVip).
		Update("status", common.UserStatusDisabled).Error)
	deletedVip := seedBatchUser(t, 624, "vip")
	require.NoError(t, DB.Delete(&User{}, deletedVip).Error)

	result, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		RecipientMode: SubscriptionResetCardRecipientsFilter,
		Filter:        SubscriptionResetCardRecipientFilter{Group: "vip"},
		Count:         1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopePlan,
			PlanId:          plan.Id,
			ResetAmountUsed: true,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, SubscriptionResetCardRecipientsFilter, result.RecipientMode)
	assert.Equal(t, 2, result.ResolvedUserCount, "only the enabled vip users match")
	assert.Equal(t, 2, result.Requested)
	assert.Equal(t, 2, result.Succeeded)

	got := make(map[int]struct{}, len(result.Items))
	for _, item := range result.Items {
		require.True(t, item.Ok, "item %+v", item)
		got[item.UserId] = struct{}{}
	}
	assert.Contains(t, got, vipA)
	assert.Contains(t, got, vipB)
	assert.NotContains(t, got, disabledVip)
	assert.NotContains(t, got, deletedVip)
}

// "All users" is the filter with nothing set: every enabled user, nobody else.
func TestBatchGrantSubscriptionResetCardsResolvesAllUsers(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9711, "All plan", 1000)
	enabledA := seedBatchUser(t, 630, "default")
	enabledB := seedBatchUser(t, 631, "vip")
	disabled := seedBatchUser(t, 632, "default")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", disabled).
		Update("status", common.UserStatusDisabled).Error)

	result, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		RecipientMode: SubscriptionResetCardRecipientsAll,
		// Conditions left in the payload must not narrow an "all" request.
		Filter: SubscriptionResetCardRecipientFilter{Group: "vip", Role: common.RoleAdminUser},
		Count:  1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopePlan,
			PlanId:          plan.Id,
			ResetAmountUsed: true,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Succeeded)
	got := make(map[int]struct{}, len(result.Items))
	for _, item := range result.Items {
		got[item.UserId] = struct{}{}
	}
	assert.Contains(t, got, enabledA)
	assert.Contains(t, got, enabledB)
	assert.NotContains(t, got, disabled)
}

// A filter that matches more users than one batch allows is refused outright:
// truncating it would silently grant cards to an arbitrary subset.
func TestBatchGrantSubscriptionResetCardsRejectsOversizedFilter(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9712, "Big plan", 1000)
	// One more user than a single batch may address.
	ids := make([]int, 0, subscriptionBatchMax+1)
	for i := 0; i <= subscriptionBatchMax; i++ {
		ids = append(ids, seedBatchUser(t, 2000+i, "bulk"))
	}

	_, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		RecipientMode: SubscriptionResetCardRecipientsFilter,
		Filter:        SubscriptionResetCardRecipientFilter{Group: "bulk"},
		Count:         1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopePlan,
			PlanId:          plan.Id,
			ResetAmountUsed: true,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")

	var count int64
	require.NoError(t, DB.Model(&SubscriptionResetCard{}).Count(&count).Error)
	assert.Zero(t, count, "an over-limit request writes nothing")
}

// A deleted status is not a recipient set; it is a mistake worth naming.
func TestResolveRecipientIdsRejectsDeletedStatus(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	_, err := resolveRecipientIds(SubscriptionResetCardRecipientFilter{Status: -1}, subscriptionBatchMax)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "已删除")
}

// Ownerless codes carry no user id, and any signed-in user holding the key can
// spend one. A second attempt with the same key must not reset anything again.
func TestOwnerlessResetCodeIsRedeemableByAnyUser(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9713, "Code plan", 1000)
	seedResetCardLimits(t, plan)
	holder := seedBatchUser(t, 640, "default")
	other := seedBatchUser(t, 641, "default")
	holderSub := seedResetCardSubscription(t, holder, plan, GetDBTimestamp()+30*24*3600)

	granted, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		RecipientMode: SubscriptionResetCardRecipientsCodes,
		Count:         2,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopePlan,
			PlanId:          plan.Id,
			ResetAmountUsed: true,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, SubscriptionResetCardRecipientsCodes, granted.RecipientMode)
	assert.Zero(t, granted.ResolvedUserCount, "codes have no recipients")
	require.Len(t, granted.Items, 1)
	require.True(t, granted.Items[0].Ok)
	require.Len(t, granted.Items[0].Keys, 2)

	codeKey := granted.Items[0].Keys[0]
	card := loadResetCard(t, granted.Items[0].CardIds[0])
	assert.Zero(t, card.UserId, "an ownerless code belongs to nobody")
	assert.Equal(t, SubscriptionResetCardStatusUnused, card.Status)

	// The holder redeems one code by key.
	used, err := RedeemSubscriptionResetCard(holder, 0, codeKey)
	require.NoError(t, err)
	assert.Equal(t, card.Id, used.CardId)
	assert.Zero(t, getSubscriptionResetSub(t, holderSub).AmountUsed)
	spent := loadResetCard(t, card.Id)
	assert.Equal(t, SubscriptionResetCardStatusUsed, spent.Status)
	assert.Equal(t, holder, spent.UsedByUserId)

	// The same key is refused the second time.
	_, err = RedeemSubscriptionResetCard(other, 0, codeKey)
	assert.ErrorIs(t, err, ErrSubscriptionResetCardUsed)
}

// An ownerless code must be reachable by key only. Accepting its id would let
// anyone enumerate ids and burn codes they were never given.
func TestOwnerlessResetCodeCannotBeRedeemedById(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9714, "Id probe plan", 1000)
	userId := seedBatchUser(t, 650, "default")
	seedResetCardSubscription(t, userId, plan, GetDBTimestamp()+30*24*3600)

	granted, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		RecipientMode: SubscriptionResetCardRecipientsCodes,
		Count:         1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopeAll,
			ResetAmountUsed: true,
		},
	})
	require.NoError(t, err)
	cardId := granted.Items[0].CardIds[0]

	_, err = RedeemSubscriptionResetCard(userId, cardId, "")
	assert.ErrorIs(t, err, ErrSubscriptionResetCardNotFound)
	assert.Equal(t, SubscriptionResetCardStatusUnused, loadResetCard(t, cardId).Status,
		"a rejected id probe must not spend the code")
}

// A subscription-scoped code could never match for anyone but one user, so the
// server refuses to mint one instead of producing a dead card.
func TestGrantResetCodesRejectsSubscriptionScope(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	plan := seedResetCardPlan(t, 9715, "Scope guard plan", 1000)
	userId := seedBatchUser(t, 660, "default")
	subId := seedResetCardSubscription(t, userId, plan, GetDBTimestamp()+30*24*3600)

	_, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		RecipientMode: SubscriptionResetCardRecipientsCodes,
		Count:         1,
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:        SubscriptionResetCardScopeSubscription,
			UserSubscriptionId: subId,
			ResetAmountUsed:    true,
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "兑换码不能指定单个订阅")

	var count int64
	require.NoError(t, DB.Model(&SubscriptionResetCard{}).Count(&count).Error)
	assert.Zero(t, count)
}

// An unknown recipient mode is a client bug, not a default.
func TestBatchGrantSubscriptionResetCardsRejectsUnknownRecipientMode(t *testing.T) {
	setupResetCardTestDatabase(t)
	truncateTables(t)

	_, err := BatchGrantSubscriptionResetCards(SubscriptionResetCardGrantRequest{
		RecipientMode: "everyone-lol",
		SubscriptionResetCardConfig: SubscriptionResetCardConfig{
			TargetScope:     SubscriptionResetCardScopeAll,
			ResetAmountUsed: true,
		},
	})
	assert.Error(t, err)
}
