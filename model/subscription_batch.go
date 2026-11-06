package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Subscription management used to be reachable only one user at a time, from
// that user's row in the admin users table. This file adds the cross-user
// queries and batch mutations an administrator needs to run a plan: list every
// subscription with owner and plan names, grant a plan to many users, and
// invalidate, delete or reset many subscriptions at once.
//
// Batch mutations reuse the single-subscription paths rather than reimplementing
// them, so group upgrade/downgrade and cache invalidation cannot drift. Each id
// is applied in its own transaction and reported individually: an admin batch is
// a list of independent operations, and one user hitting their purchase cap must
// not roll back the other ninety-nine.

// subscriptionBatchMax bounds one batch request. Each entry runs a full
// transaction with row locks, so an unbounded list would hold the request open
// for as long as the caller asked for.
const subscriptionBatchMax = 500

// ErrSubscriptionBatchEmpty is returned when a batch request names no target.
var ErrSubscriptionBatchEmpty = errors.New("no subscription selected")

// ErrSubscriptionBatchTooLarge is returned when a batch request exceeds
// subscriptionBatchMax entries.
var ErrSubscriptionBatchTooLarge = fmt.Errorf("a batch operation accepts at most %d entries", subscriptionBatchMax)

// UserSubscriptionListFilter narrows ListUserSubscriptions. Every field is
// optional; a zero value means "do not filter on this".
type UserSubscriptionListFilter struct {
	UserId int
	PlanId int
	// Status matches user_subscriptions.status exactly (active/expired/cancelled).
	Status string
	// Keyword matches a username, a display name, or a literal user id.
	Keyword string
	// ActiveOnly keeps subscriptions that are active and not yet past end_time.
	ActiveOnly bool
}

// UserSubscriptionListItem is one row of the cross-user admin list. The owner and
// plan names are resolved server-side so the table does not need one request per
// row to label itself.
type UserSubscriptionListItem struct {
	Subscription *UserSubscription        `json:"subscription"`
	UserId       int                      `json:"user_id"`
	Username     string                   `json:"username"`
	PlanId       int                      `json:"plan_id"`
	PlanTitle    string                   `json:"plan_title"`
	QuotaLimits  []SubscriptionLimitUsage `json:"quota_limits,omitempty"`
	// AllowedModels is the plan's model whitelist; omitted when the plan covers
	// every model.
	AllowedModels []string `json:"allowed_models,omitempty"`
}

// ListUserSubscriptions returns one page of subscriptions across all users, plus
// the total matching count. Rows are ordered newest first so an administrator
// sees what was just granted.
func ListUserSubscriptions(filter UserSubscriptionListFilter, pageInfo *common.PageInfo) ([]UserSubscriptionListItem, int64, error) {
	if pageInfo == nil {
		return nil, 0, errors.New("pageInfo is required")
	}

	query := DB.Model(&UserSubscription{})
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.PlanId > 0 {
		query = query.Where("plan_id = ?", filter.PlanId)
	}
	if status := filter.Status; status != "" {
		query = query.Where("status = ?", status)
	}
	if filter.ActiveOnly {
		query = query.Where("status = ? AND end_time > ?", "active", GetDBTimestamp())
	}
	if keyword := filter.Keyword; keyword != "" {
		// A numeric keyword also matches the user id, like the users table does.
		usersQuery := DB.Model(&User{}).Select("id").
			Where("username LIKE ? OR display_name LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
		if userId, err := parsePositiveInt(keyword); err == nil {
			usersQuery = DB.Model(&User{}).Select("id").
				Where("id = ? OR username LIKE ? OR display_name LIKE ?", userId, "%"+keyword+"%", "%"+keyword+"%")
		}
		query = query.Where("user_id IN (?)", usersQuery)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []UserSubscriptionListItem{}, 0, nil
	}

	var subs []UserSubscription
	if err := query.
		Order("id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&subs).Error; err != nil {
		return nil, 0, err
	}

	items := make([]UserSubscriptionListItem, 0, len(subs))
	usernames := loadUsernamesByIds(collectSubscriptionUserIds(subs))
	now := GetDBTimestamp()
	for i := range subs {
		sub := subs[i]
		item := UserSubscriptionListItem{
			Subscription: &sub,
			UserId:       sub.UserId,
			Username:     usernames[sub.UserId],
			PlanId:       sub.PlanId,
		}
		if plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId); err == nil {
			item.PlanTitle = plan.Title
			item.QuotaLimits = buildSubscriptionLimitUsage(plan, &sub, now)
			item.AllowedModels = plan.GetAllowedModels()
		}
		items = append(items, item)
	}
	return items, total, nil
}

func collectSubscriptionUserIds(subs []UserSubscription) []int {
	seen := make(map[int]struct{}, len(subs))
	ids := make([]int, 0, len(subs))
	for _, sub := range subs {
		if _, ok := seen[sub.UserId]; ok {
			continue
		}
		seen[sub.UserId] = struct{}{}
		ids = append(ids, sub.UserId)
	}
	return ids
}

func loadUsernamesByIds(userIds []int) map[int]string {
	names := make(map[int]string, len(userIds))
	if len(userIds) == 0 {
		return names
	}
	var rows []struct {
		Id       int    `gorm:"column:id"`
		Username string `gorm:"column:username"`
	}
	// A missing user row leaves that entry unnamed rather than failing the page.
	_ = DB.Model(&User{}).Select("id", "username").Where("id IN ?", userIds).Find(&rows).Error
	for _, row := range rows {
		names[row.Id] = row.Username
	}
	return names
}

// parsePositiveInt reads a keyword that is meant to be a literal user id. A
// non-numeric or non-positive keyword simply is not an id, so the caller falls
// back to the text match.
func parsePositiveInt(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, errors.New("not a positive id")
	}
	return parsed, nil
}

// ---------------------------------------------------------------------------
// Batch mutations
// ---------------------------------------------------------------------------

// SubscriptionBatchItemOutcome reports what happened to one entry of a batch.
// Ok false carries the reason so the UI can show a partial success honestly.
type SubscriptionBatchItemOutcome struct {
	Id      int    `json:"id"`
	UserId  int    `json:"user_id,omitempty"`
	PlanId  int    `json:"plan_id,omitempty"`
	Ok      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// SubscriptionBatchResult summarizes a batch mutation.
type SubscriptionBatchResult struct {
	Requested int                            `json:"requested"`
	Succeeded int                            `json:"succeeded"`
	Failed    int                            `json:"failed"`
	Items     []SubscriptionBatchItemOutcome `json:"items"`
}

// validateSubscriptionBatchInput applies the shared bounds to any batch request.
func validateSubscriptionBatchInput(ids []int) error {
	if len(ids) == 0 {
		return ErrSubscriptionBatchEmpty
	}
	if len(ids) > subscriptionBatchMax {
		return ErrSubscriptionBatchTooLarge
	}
	return nil
}

// BatchInvalidateUserSubscriptions cancels each subscription by id, ending it now
// and applying the plan's group downgrade, exactly as the single-subscription
// admin action does.
func BatchInvalidateUserSubscriptions(ids []int) (*SubscriptionBatchResult, error) {
	if err := validateSubscriptionBatchInput(ids); err != nil {
		return nil, err
	}
	result := &SubscriptionBatchResult{Requested: len(ids), Items: make([]SubscriptionBatchItemOutcome, 0, len(ids))}
	for _, id := range ids {
		outcome := SubscriptionBatchItemOutcome{Id: id}
		message, err := AdminInvalidateUserSubscription(id)
		if err != nil {
			outcome.Message = err.Error()
			result.Failed++
		} else {
			outcome.Ok = true
			outcome.Message = message
			result.Succeeded++
		}
		result.Items = append(result.Items, outcome)
	}
	return result, nil
}

// BatchDeleteUserSubscriptions hard-deletes each subscription by id, applying the
// same group downgrade the single-subscription delete does.
func BatchDeleteUserSubscriptions(ids []int) (*SubscriptionBatchResult, error) {
	if err := validateSubscriptionBatchInput(ids); err != nil {
		return nil, err
	}
	result := &SubscriptionBatchResult{Requested: len(ids), Items: make([]SubscriptionBatchItemOutcome, 0, len(ids))}
	for _, id := range ids {
		outcome := SubscriptionBatchItemOutcome{Id: id}
		message, err := AdminDeleteUserSubscription(id)
		if err != nil {
			outcome.Message = err.Error()
			result.Failed++
		} else {
			outcome.Ok = true
			outcome.Message = message
			result.Succeeded++
		}
		result.Items = append(result.Items, outcome)
	}
	return result, nil
}

// BatchResetUserSubscriptions zeroes each subscription's usage counters. The
// plan's reset cycle governs which counters are cleared, and advanceResetTime
// moves the next reset forward, matching the existing per-plan reset dialog.
func BatchResetUserSubscriptions(ids []int, advanceResetTime bool) (*SubscriptionBatchResult, error) {
	if err := validateSubscriptionBatchInput(ids); err != nil {
		return nil, err
	}
	result := &SubscriptionBatchResult{Requested: len(ids), Items: make([]SubscriptionBatchItemOutcome, 0, len(ids))}
	now := GetDBTimestamp()
	for _, id := range ids {
		outcome := SubscriptionBatchItemOutcome{Id: id}
		err := DB.Transaction(func(tx *gorm.DB) error {
			var sub UserSubscription
			if err := lockForUpdate(tx).Where("id = ?", id).First(&sub).Error; err != nil {
				return err
			}
			plan, err := getSubscriptionPlanByIdTx(tx, sub.PlanId)
			if err != nil {
				return err
			}
			return resetUserSubscriptionTx(tx, &sub, plan, now, advanceResetTime)
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

// BatchGrantSubscriptionToUsers grants planId to each user id, one transaction per
// user so a purchase cap or a missing user only fails that entry.
func BatchGrantSubscriptionToUsers(userIds []int, planId int) (*SubscriptionBatchResult, error) {
	if err := validateSubscriptionBatchInput(userIds); err != nil {
		return nil, err
	}
	if planId <= 0 {
		return nil, errors.New("invalid planId")
	}
	if _, err := GetSubscriptionPlanById(planId); err != nil {
		return nil, err
	}
	result := &SubscriptionBatchResult{Requested: len(userIds), Items: make([]SubscriptionBatchItemOutcome, 0, len(userIds))}
	for _, userId := range userIds {
		outcome := SubscriptionBatchItemOutcome{UserId: userId, PlanId: planId}
		message, err := AdminBindSubscription(userId, planId, "")
		if err != nil {
			outcome.Message = err.Error()
			result.Failed++
		} else {
			outcome.Ok = true
			outcome.Message = message
			result.Succeeded++
		}
		result.Items = append(result.Items, outcome)
	}
	return result, nil
}
