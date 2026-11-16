package model

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// A subscription reset card is a one-shot voucher an administrator hands to a
// user. Redeeming it resets the usage counters of that user's subscriptions
// according to the configuration baked into the card when it was granted.
//
// Every card is independent: it carries its own target scope, its own selection
// of counters, its own expiry, status and redemption record. Cards granted in
// one batch share a BatchId so an operator can still trace them back to the
// request that created them, but nothing about one card's lifecycle depends on
// another's.
const (
	// SubscriptionResetCardScopeAll targets every active subscription the
	// holder has at redemption time.
	SubscriptionResetCardScopeAll = "all"
	// SubscriptionResetCardScopePlan targets the holder's active subscriptions
	// to one plan.
	SubscriptionResetCardScopePlan = "plan"
	// SubscriptionResetCardScopeSubscription targets exactly one subscription,
	// which must belong to the holder and still be active.
	SubscriptionResetCardScopeSubscription = "subscription"

	SubscriptionResetCardStatusUnused   = "unused"
	SubscriptionResetCardStatusUsed     = "used"
	SubscriptionResetCardStatusDisabled = "disabled"
)

const (
	// subscriptionResetCardGrantMaxPerUser bounds how many cards one batch may
	// mint for a single user.
	subscriptionResetCardGrantMaxPerUser = 100
	// subscriptionResetCardGrantMaxTotal bounds the whole batch: every card is
	// its own row, and an unbounded request would let one call fill the table.
	subscriptionResetCardGrantMaxTotal = 5000
	subscriptionResetCardNameMax       = 64
)

var (
	// ErrSubscriptionResetCardNotFound is returned both when the id/key does not
	// exist and when it belongs to another user, so a caller cannot probe for
	// other users' card ids.
	ErrSubscriptionResetCardNotFound = errors.New("重置卡不存在")
	ErrSubscriptionResetCardUsed     = errors.New("该重置卡已被使用")
	ErrSubscriptionResetCardDisabled = errors.New("该重置卡已失效")
	ErrSubscriptionResetCardExpired  = errors.New("该重置卡已过期")
	ErrSubscriptionResetCardNoTarget = errors.New("没有可重置的有效订阅")
)

// SubscriptionResetCardLimitKeys is the card's optional narrowing of which
// quota-limit counters it clears, stored as a JSON text column. Empty means
// every counter the cycle/window switches allow.
type SubscriptionResetCardLimitKeys []string

func (l *SubscriptionResetCardLimitKeys) Scan(value any) error {
	return scanJSONSliceColumn(value, (*[]string)(l))
}

func (l SubscriptionResetCardLimitKeys) Value() (driver.Value, error) {
	return valueJSONSliceColumn(l)
}

// SubscriptionResetCard is one granted voucher.
type SubscriptionResetCard struct {
	Id     int    `json:"id"`
	UserId int    `json:"user_id" gorm:"index"`
	Name   string `json:"name" gorm:"type:varchar(64);index"`
	Key    string `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status string `json:"status" gorm:"type:varchar(32);index"`
	// BatchId groups the cards minted by one grant request.
	BatchId string `json:"batch_id" gorm:"type:char(32);index"`

	// The reset configuration, frozen at grant time. Editing a plan later does
	// not change what an outstanding card does; only its target list is resolved
	// at redemption time.
	TargetScope        string                         `json:"target_scope" gorm:"type:varchar(32)"`
	PlanId             int                            `json:"plan_id" gorm:"default:0"`
	UserSubscriptionId int                            `json:"user_subscription_id" gorm:"default:0"`
	LimitKeys          SubscriptionResetCardLimitKeys `json:"limit_keys,omitempty" gorm:"type:text"`
	ResetAmountUsed    bool                           `json:"reset_amount_used"`
	ResetCycleLimits   bool                           `json:"reset_cycle_limits"`
	ResetWindowLimits  bool                           `json:"reset_window_limits"`
	AdvanceResetTime   bool                           `json:"advance_reset_time"`

	ExpiredTime  int64 `json:"expired_time" gorm:"bigint;default:0"`
	UsedTime     int64 `json:"used_time" gorm:"bigint;default:0"`
	UsedByUserId int   `json:"used_by_user_id" gorm:"default:0"`

	CreatedTime int64 `json:"created_time" gorm:"bigint"`
	UpdatedTime int64 `json:"updated_time" gorm:"bigint"`
}

func (card *SubscriptionResetCard) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	card.CreatedTime = now
	card.UpdatedTime = now
	return nil
}

func (card *SubscriptionResetCard) BeforeUpdate(tx *gorm.DB) error {
	card.UpdatedTime = common.GetTimestamp()
	return nil
}

// SubscriptionResetCardConfig is the configuration an operator sets when
// granting a card. It mirrors SubscriptionResetScope and adds where the reset
// applies.
type SubscriptionResetCardConfig struct {
	TargetScope        string   `json:"target_scope"`
	PlanId             int      `json:"plan_id"`
	UserSubscriptionId int      `json:"user_subscription_id"`
	LimitKeys          []string `json:"limit_keys"`
	ResetAmountUsed    bool     `json:"reset_amount_used"`
	ResetCycleLimits   bool     `json:"reset_cycle_limits"`
	ResetWindowLimits  bool     `json:"reset_window_limits"`
	AdvanceResetTime   bool     `json:"advance_reset_time"`
}

// Normalize fills in the default scope and drops fields that the selected scope
// cannot use, so a stored card never carries a plan id it will never match.
func (c SubscriptionResetCardConfig) Normalize() SubscriptionResetCardConfig {
	c.TargetScope = strings.ToLower(strings.TrimSpace(c.TargetScope))
	if c.TargetScope == "" {
		c.TargetScope = SubscriptionResetCardScopeAll
	}
	c.LimitKeys = SubscriptionResetScope{LimitKeys: c.LimitKeys}.Normalize().LimitKeys
	if c.TargetScope != SubscriptionResetCardScopePlan {
		c.PlanId = 0
	}
	if c.TargetScope != SubscriptionResetCardScopeSubscription {
		c.UserSubscriptionId = 0
	}
	return c
}

// Validate rejects a configuration that could never do anything: an unknown
// scope, a missing target, or no selected counter.
func (c SubscriptionResetCardConfig) Validate() error {
	switch c.TargetScope {
	case SubscriptionResetCardScopeAll:
	case SubscriptionResetCardScopePlan:
		if c.PlanId <= 0 {
			return errors.New("指定套餐的重置卡必须选择套餐")
		}
	case SubscriptionResetCardScopeSubscription:
		if c.UserSubscriptionId <= 0 {
			return errors.New("指定订阅的重置卡必须选择订阅")
		}
	default:
		return errors.New("无效的重置范围")
	}
	return c.Scope().Validate()
}

// Scope converts the card configuration into the reset scope the shared reset
// path consumes.
func (c SubscriptionResetCardConfig) Scope() SubscriptionResetScope {
	return SubscriptionResetScope{
		AdvanceResetTime:  c.AdvanceResetTime,
		ResetAmountUsed:   c.ResetAmountUsed,
		ResetCycleLimits:  c.ResetCycleLimits,
		ResetWindowLimits: c.ResetWindowLimits,
		LimitKeys:         append([]string(nil), c.LimitKeys...),
	}.Normalize()
}

// matchesSubscription reports whether the card targets one subscription. The
// caller has already filtered to the holder's active subscriptions.
func (c SubscriptionResetCardConfig) matchesSubscription(sub *UserSubscription) bool {
	if sub == nil {
		return false
	}
	switch c.TargetScope {
	case SubscriptionResetCardScopeAll:
		return true
	case SubscriptionResetCardScopePlan:
		return sub.PlanId == c.PlanId
	case SubscriptionResetCardScopeSubscription:
		return sub.Id == c.UserSubscriptionId
	default:
		return false
	}
}

// Config returns the configuration stored on this card.
func (card *SubscriptionResetCard) Config() SubscriptionResetCardConfig {
	if card == nil {
		return SubscriptionResetCardConfig{}
	}
	return SubscriptionResetCardConfig{
		TargetScope:        card.TargetScope,
		PlanId:             card.PlanId,
		UserSubscriptionId: card.UserSubscriptionId,
		LimitKeys:          append([]string(nil), card.LimitKeys...),
		ResetAmountUsed:    card.ResetAmountUsed,
		ResetCycleLimits:   card.ResetCycleLimits,
		ResetWindowLimits:  card.ResetWindowLimits,
		AdvanceResetTime:   card.AdvanceResetTime,
	}
}

// SubscriptionResetCardListItem is one row of the admin card list: the card
// plus the holder's username, resolved server-side.
type SubscriptionResetCardListItem struct {
	Card     SubscriptionResetCard `json:"card"`
	Username string                `json:"username"`
}

// SubscriptionResetCardListFilter narrows the admin card list.
type SubscriptionResetCardListFilter struct {
	UserId  int
	Status  string
	BatchId string
	// Keyword matches a card name, a card key, or the holder's username.
	Keyword string
}

// ListSubscriptionResetCards returns one page of cards, newest first.
func ListSubscriptionResetCards(filter SubscriptionResetCardListFilter, pageInfo *common.PageInfo) ([]SubscriptionResetCardListItem, int64, error) {
	if pageInfo == nil {
		return nil, 0, errors.New("pageInfo is required")
	}
	query := DB.Model(&SubscriptionResetCard{})
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if batchId := strings.TrimSpace(filter.BatchId); batchId != "" {
		query = query.Where("batch_id = ?", batchId)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		userIdsQuery := DB.Model(&User{}).Select("id").
			Where("username LIKE ? OR display_name LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
		query = query.Where(
			"name LIKE ? OR "+commonKeyCol+" = ? OR user_id IN (?)",
			"%"+keyword+"%", keyword, userIdsQuery,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []SubscriptionResetCardListItem{}, 0, nil
	}

	var cards []SubscriptionResetCard
	if err := query.Order("id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&cards).Error; err != nil {
		return nil, 0, err
	}

	userIds := make([]int, 0, len(cards))
	for _, card := range cards {
		userIds = append(userIds, card.UserId)
	}
	usernames := loadUsernamesByIds(userIds)
	items := make([]SubscriptionResetCardListItem, 0, len(cards))
	for _, card := range cards {
		items = append(items, SubscriptionResetCardListItem{
			Card:     card,
			Username: usernames[card.UserId],
		})
	}
	return items, total, nil
}

// ListUserSubscriptionResetCards returns every card held by one user, newest
// first, so the wallet can show both usable and spent cards.
func ListUserSubscriptionResetCards(userId int) ([]SubscriptionResetCard, error) {
	if userId <= 0 {
		return nil, errors.New("无效的 user id")
	}
	var cards []SubscriptionResetCard
	if err := DB.Where("user_id = ?", userId).Order("id desc").Find(&cards).Error; err != nil {
		return nil, err
	}
	return cards, nil
}

// SubscriptionResetCardGrantRequest asks for count cards per user, all carrying
// the same configuration.
type SubscriptionResetCardGrantRequest struct {
	UserIds []int  `json:"user_ids"`
	Count   int    `json:"count"`
	Name    string `json:"name"`
	// ExpiredTime is a Unix timestamp; 0 means the cards never expire.
	ExpiredTime int64 `json:"expired_time"`
	SubscriptionResetCardConfig
}

// SubscriptionResetCardGrantItem reports what was minted for one user.
type SubscriptionResetCardGrantItem struct {
	UserId  int      `json:"user_id"`
	Ok      bool     `json:"ok"`
	Message string   `json:"message,omitempty"`
	CardIds []int    `json:"card_ids,omitempty"`
	Keys    []string `json:"keys,omitempty"`
}

// SubscriptionResetCardGrantResult summarizes a grant request.
type SubscriptionResetCardGrantResult struct {
	Requested int                              `json:"requested"`
	Succeeded int                              `json:"succeeded"`
	Failed    int                              `json:"failed"`
	Count     int                              `json:"count"`
	BatchId   string                           `json:"batch_id"`
	Items     []SubscriptionResetCardGrantItem `json:"items"`
}

// uniquePositiveInts drops non-positive and duplicate user ids, preserving the
// caller's order so the per-user outcomes line up with the request.
func uniquePositiveInts(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// defaultSubscriptionResetCardName labels a card an operator left unnamed.
func defaultSubscriptionResetCardName(cfg SubscriptionResetCardConfig) string {
	switch cfg.TargetScope {
	case SubscriptionResetCardScopePlan:
		return fmt.Sprintf("套餐 #%d 重置卡", cfg.PlanId)
	case SubscriptionResetCardScopeSubscription:
		return fmt.Sprintf("订阅 #%d 重置卡", cfg.UserSubscriptionId)
	default:
		return "全部订阅重置卡"
	}
}

// BatchGrantSubscriptionResetCards mints count cards for each user, one
// transaction per user so a missing user only fails that entry. Every card in
// the batch shares one BatchId.
func BatchGrantSubscriptionResetCards(req SubscriptionResetCardGrantRequest) (*SubscriptionResetCardGrantResult, error) {
	userIds := uniquePositiveInts(req.UserIds)
	if err := validateSubscriptionBatchInput(userIds); err != nil {
		return nil, err
	}
	count := req.Count
	if count <= 0 {
		count = 1
	}
	if count > subscriptionResetCardGrantMaxPerUser {
		return nil, fmt.Errorf("每个用户最多发放 %d 张重置卡", subscriptionResetCardGrantMaxPerUser)
	}
	if len(userIds)*count > subscriptionResetCardGrantMaxTotal {
		return nil, fmt.Errorf("单次最多发放 %d 张重置卡", subscriptionResetCardGrantMaxTotal)
	}
	cfg := req.SubscriptionResetCardConfig.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.TargetScope == SubscriptionResetCardScopePlan {
		if _, err := getSubscriptionPlanByIdTx(nil, cfg.PlanId); err != nil {
			return nil, errors.New("指定的套餐不存在")
		}
	}
	if cfg.TargetScope == SubscriptionResetCardScopeSubscription {
		var sub UserSubscription
		if err := DB.Where("id = ?", cfg.UserSubscriptionId).First(&sub).Error; err != nil {
			return nil, errors.New("指定的订阅不存在")
		}
		for _, userId := range userIds {
			if userId != sub.UserId {
				return nil, errors.New("指定订阅的重置卡只能发放给该订阅的持有人")
			}
		}
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = defaultSubscriptionResetCardName(cfg)
	}
	if runes := []rune(name); len(runes) > subscriptionResetCardNameMax {
		name = string(runes[:subscriptionResetCardNameMax])
	}

	batchId := common.GetUUID()
	result := &SubscriptionResetCardGrantResult{
		Requested: len(userIds),
		Count:     count,
		BatchId:   batchId,
		Items:     make([]SubscriptionResetCardGrantItem, 0, len(userIds)),
	}
	for _, userId := range userIds {
		outcome := SubscriptionResetCardGrantItem{UserId: userId}
		err := DB.Transaction(func(tx *gorm.DB) error {
			var user User
			if err := tx.Select("id").Where("id = ?", userId).First(&user).Error; err != nil {
				return errors.New("用户不存在")
			}
			now := GetDBTimestampTx(tx)
			cards := make([]SubscriptionResetCard, 0, count)
			for i := 0; i < count; i++ {
				card := SubscriptionResetCard{
					UserId:             userId,
					Name:               name,
					Key:                common.GetUUID(),
					Status:             SubscriptionResetCardStatusUnused,
					BatchId:            batchId,
					TargetScope:        cfg.TargetScope,
					PlanId:             cfg.PlanId,
					UserSubscriptionId: cfg.UserSubscriptionId,
					LimitKeys:          SubscriptionResetCardLimitKeys(cfg.LimitKeys),
					ResetAmountUsed:    cfg.ResetAmountUsed,
					ResetCycleLimits:   cfg.ResetCycleLimits,
					ResetWindowLimits:  cfg.ResetWindowLimits,
					AdvanceResetTime:   cfg.AdvanceResetTime,
					ExpiredTime:        req.ExpiredTime,
					CreatedTime:        now,
					UpdatedTime:        now,
				}
				if err := tx.Create(&card).Error; err != nil {
					return err
				}
				cards = append(cards, card)
			}
			outcome.Ok = true
			outcome.CardIds = make([]int, 0, len(cards))
			outcome.Keys = make([]string, 0, len(cards))
			for _, card := range cards {
				outcome.CardIds = append(outcome.CardIds, card.Id)
				outcome.Keys = append(outcome.Keys, card.Key)
			}
			return nil
		})
		if err != nil {
			outcome.Message = err.Error()
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Items = append(result.Items, outcome)
	}
	return result, nil
}

// SubscriptionResetCardUseResult reports what one redemption reset.
type SubscriptionResetCardUseResult struct {
	CardId             int      `json:"card_id"`
	Name               string   `json:"name"`
	TargetScope        string   `json:"target_scope"`
	PlanId             int      `json:"plan_id,omitempty"`
	UserSubscriptionId int      `json:"user_subscription_id,omitempty"`
	MatchedCount       int      `json:"matched_count"`
	ResetCount         int      `json:"reset_count"`
	AdvanceResetTime   bool     `json:"advance_reset_time"`
	ResetAmountUsed    bool     `json:"reset_amount_used"`
	ResetCycleLimits   bool     `json:"reset_cycle_limits"`
	ResetWindowLimits  bool     `json:"reset_window_limits"`
	LimitKeys          []string `json:"limit_keys,omitempty"`
}

// RedeemSubscriptionResetCard consumes one card held by userId and applies its
// configuration. The card is located by id when cardId > 0, otherwise by key;
// both forms only ever match the caller's own cards.
//
// The card is flipped unused -> used with a compare-and-swap before any counter
// is touched, and the reset runs in the same transaction: a card that finds no
// matching subscription fails the whole transaction, so the user does not lose
// a card to a click made when they had no active subscription.
func RedeemSubscriptionResetCard(userId int, cardId int, key string) (*SubscriptionResetCardUseResult, error) {
	if userId <= 0 {
		return nil, errors.New("无效的 user id")
	}
	key = strings.TrimSpace(key)
	if cardId <= 0 && key == "" {
		return nil, errors.New("未提供重置卡")
	}
	keyCol := "`key`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		keyCol = `"key"`
	}
	common.RandomSleep()

	var result *SubscriptionResetCardUseResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		var card SubscriptionResetCard
		query := lockForUpdate(tx)
		if cardId > 0 {
			query = query.Where("id = ? AND user_id = ?", cardId, userId)
		} else {
			query = query.Where(keyCol+" = ?", key)
		}
		if err := query.First(&card).Error; err != nil {
			return ErrSubscriptionResetCardNotFound
		}
		// A card belongs to exactly one user; a key handed to someone else is
		// indistinguishable from a key that does not exist.
		if card.UserId != userId {
			return ErrSubscriptionResetCardNotFound
		}
		switch card.Status {
		case SubscriptionResetCardStatusUnused:
		case SubscriptionResetCardStatusUsed:
			return ErrSubscriptionResetCardUsed
		default:
			return ErrSubscriptionResetCardDisabled
		}
		now := GetDBTimestampTx(tx)
		if card.ExpiredTime != 0 && card.ExpiredTime < now {
			return ErrSubscriptionResetCardExpired
		}
		cfg := card.Config().Normalize()
		if err := cfg.Validate(); err != nil {
			return err
		}

		// Compare-and-swap on status: only the transaction that flips
		// unused -> used may reset, so a concurrent redemption of the same card
		// loses here even on a database without row locks (SQLite).
		update := tx.Model(&SubscriptionResetCard{}).
			Where("id = ? AND status = ?", card.Id, SubscriptionResetCardStatusUnused).
			Updates(map[string]any{
				"status":          SubscriptionResetCardStatusUsed,
				"used_time":       now,
				"used_by_user_id": userId,
				"updated_time":    now,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return ErrSubscriptionResetCardUsed
		}

		var subs []UserSubscription
		if err := lockForUpdate(tx).
			Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
			Order("end_time asc, id asc").
			Find(&subs).Error; err != nil {
			return err
		}
		scope := cfg.Scope()
		resetCount := 0
		for i := range subs {
			if !cfg.matchesSubscription(&subs[i]) {
				continue
			}
			plan, err := getSubscriptionPlanByIdTx(tx, subs[i].PlanId)
			if err != nil {
				return err
			}
			if err := resetUserSubscriptionTx(tx, &subs[i], plan, now, scope); err != nil {
				return err
			}
			resetCount++
		}
		if resetCount == 0 {
			return ErrSubscriptionResetCardNoTarget
		}
		result = &SubscriptionResetCardUseResult{
			CardId:             card.Id,
			Name:               card.Name,
			TargetScope:        cfg.TargetScope,
			PlanId:             cfg.PlanId,
			UserSubscriptionId: cfg.UserSubscriptionId,
			MatchedCount:       resetCount,
			ResetCount:         resetCount,
			AdvanceResetTime:   scope.AdvanceResetTime,
			ResetAmountUsed:    scope.ResetAmountUsed,
			ResetCycleLimits:   scope.ResetCycleLimits,
			ResetWindowLimits:  scope.ResetWindowLimits,
			LimitKeys:          scope.LimitKeys,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	RecordLog(userId, LogTypeSystem, fmt.Sprintf(
		"使用订阅重置卡「%s」重置了 %d 个订阅额度", result.Name, result.ResetCount))
	return result, nil
}

// BatchInvalidateSubscriptionResetCards disables unused cards. A card that was
// already spent keeps its status: the redemption record is evidence, not
// something an operator may rewrite by disabling the card afterwards.
func BatchInvalidateSubscriptionResetCards(ids []int) (*SubscriptionBatchResult, error) {
	if err := validateSubscriptionBatchInput(ids); err != nil {
		return nil, err
	}
	result := &SubscriptionBatchResult{Requested: len(ids), Items: make([]SubscriptionBatchItemOutcome, 0, len(ids))}
	for _, id := range ids {
		outcome := SubscriptionBatchItemOutcome{Id: id}
		err := DB.Transaction(func(tx *gorm.DB) error {
			update := tx.Model(&SubscriptionResetCard{}).
				Where("id = ? AND status = ?", id, SubscriptionResetCardStatusUnused).
				Update("status", SubscriptionResetCardStatusDisabled)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected > 0 {
				return nil
			}
			var card SubscriptionResetCard
			if err := tx.Where("id = ?", id).First(&card).Error; err != nil {
				return ErrSubscriptionResetCardNotFound
			}
			if card.Status == SubscriptionResetCardStatusUsed {
				return ErrSubscriptionResetCardUsed
			}
			// Already disabled: the requested end state already holds.
			return nil
		})
		if err != nil {
			outcome.Message = err.Error()
			result.Failed++
		} else {
			outcome.Ok = true
			result.Succeeded++
		}
		result.Items = append(result.Items, outcome)
	}
	return result, nil
}

// BatchDeleteSubscriptionResetCards removes cards permanently.
func BatchDeleteSubscriptionResetCards(ids []int) (*SubscriptionBatchResult, error) {
	if err := validateSubscriptionBatchInput(ids); err != nil {
		return nil, err
	}
	result := &SubscriptionBatchResult{Requested: len(ids), Items: make([]SubscriptionBatchItemOutcome, 0, len(ids))}
	for _, id := range ids {
		outcome := SubscriptionBatchItemOutcome{Id: id}
		deleteResult := DB.Where("id = ?", id).Delete(&SubscriptionResetCard{})
		if deleteResult.Error != nil {
			outcome.Message = deleteResult.Error.Error()
			result.Failed++
		} else if deleteResult.RowsAffected == 0 {
			outcome.Message = ErrSubscriptionResetCardNotFound.Error()
			result.Failed++
		} else {
			outcome.Ok = true
			result.Succeeded++
		}
		result.Items = append(result.Items, outcome)
	}
	return result, nil
}
