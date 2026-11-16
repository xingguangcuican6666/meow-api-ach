package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
