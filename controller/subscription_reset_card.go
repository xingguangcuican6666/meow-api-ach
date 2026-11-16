package controller

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// Subscription reset cards
// ---------------------------------------------------------------------------
//
// A reset card is a one-shot voucher that resets a user's subscription counters
// according to the configuration frozen on it at grant time. Root issues them,
// the holder redeems them; every card is independent and one grant request may
// mint many cards for many users at once.

// AdminGrantSubscriptionResetCardRequest is the grant payload. The embedded
// config keeps the wire shape flat, matching the other subscription requests.
type AdminGrantSubscriptionResetCardRequest struct {
	UserIds []int `json:"user_ids"`
	// RecipientMode is one of users/filter/all/codes; empty means users.
	RecipientMode string                                     `json:"recipient_mode"`
	Filter        model.SubscriptionResetCardRecipientFilter `json:"filter"`
	Count         int                                        `json:"count"`
	Name          string                                     `json:"name"`
	// ExpiredTime is a Unix timestamp; 0 means the cards never expire.
	ExpiredTime int64 `json:"expired_time"`
	model.SubscriptionResetCardConfig
}

// resolveSubscriptionResetCardConfig normalizes and validates the card config
// shared by grant and preview paths.
func resolveSubscriptionResetCardConfig(req *AdminGrantSubscriptionResetCardRequest) (model.SubscriptionResetCardConfig, error) {
	cfg := req.SubscriptionResetCardConfig.Normalize()
	if err := cfg.Validate(); err != nil {
		if errors.Is(err, model.ErrSubscriptionResetScopeEmpty) {
			return cfg, errors.New("至少需要选择一项重置内容")
		}
		return cfg, err
	}
	return cfg, nil
}

// subscriptionResetCardAudit renders a card config for audit entries.
func subscriptionResetCardAudit(cfg model.SubscriptionResetCardConfig) map[string]any {
	params := map[string]any{
		"target_scope": cfg.TargetScope,
	}
	if cfg.PlanId > 0 {
		params["plan_id"] = cfg.PlanId
	}
	if cfg.UserSubscriptionId > 0 {
		params["user_subscription_id"] = cfg.UserSubscriptionId
	}
	for key, value := range subscriptionResetScopeAudit(cfg.Scope()) {
		params[key] = value
	}
	return params
}

// AdminGrantSubscriptionResetCards mints reset cards for the recipients named by
// the request: an explicit user list, a user filter, every enabled user, or an
// ownerless batch of redeem codes.
func AdminGrantSubscriptionResetCards(c *gin.Context) {
	var req AdminGrantSubscriptionResetCardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	cfg, err := resolveSubscriptionResetCardConfig(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.BatchGrantSubscriptionResetCards(model.SubscriptionResetCardGrantRequest{
		UserIds:                     req.UserIds,
		RecipientMode:               req.RecipientMode,
		Filter:                      req.Filter,
		Count:                       req.Count,
		Name:                        strings.TrimSpace(req.Name),
		ExpiredTime:                 req.ExpiredTime,
		SubscriptionResetCardConfig: cfg,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	params := subscriptionResetCardAudit(cfg)
	params["recipient_mode"] = result.RecipientMode
	params["requested"] = result.Requested
	params["succeeded"] = result.Succeeded
	params["failed"] = result.Failed
	params["count"] = result.Count
	params["batch_id"] = result.BatchId
	if result.ResolvedUserCount > 0 {
		params["resolved_user_count"] = result.ResolvedUserCount
	}
	// A filter grant is only reproducible if the conditions that selected the
	// recipients are on the record; the resolved ids are not enough on their own.
	if req.RecipientMode == model.SubscriptionResetCardRecipientsFilter {
		if keyword := strings.TrimSpace(req.Filter.Keyword); keyword != "" {
			params["filter_keyword"] = keyword
		}
		if group := strings.TrimSpace(req.Filter.Group); group != "" {
			params["filter_group"] = group
		}
		if req.Filter.Role > 0 {
			params["filter_role"] = req.Filter.Role
		}
		if req.Filter.Status != 0 {
			params["filter_status"] = req.Filter.Status
		}
	}
	if len(req.UserIds) > 0 {
		params["requested_user_ids"] = append([]int{}, req.UserIds[:min(len(req.UserIds), subscriptionBatchAuditMaxIds)]...)
		if len(req.UserIds) > subscriptionBatchAuditMaxIds {
			params["requested_user_ids_truncated"] = true
		}
	}
	action := "subscription.reset_card_grant"
	if result.RecipientMode == model.SubscriptionResetCardRecipientsCodes {
		action = "subscription.reset_card_generate_codes"
	}
	recordManageAudit(c, action, params)
	common.ApiSuccess(c, result)
}

// AdminListSubscriptionResetCards lists cards across users.
func AdminListSubscriptionResetCards(c *gin.Context) {
	filter := model.SubscriptionResetCardListFilter{
		Status:  strings.TrimSpace(c.Query("status")),
		BatchId: strings.TrimSpace(c.Query("batch_id")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
	}
	if userId, err := strconv.Atoi(c.Query("user_id")); err == nil {
		filter.UserId = userId
	}
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.ListSubscriptionResetCards(filter, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

// respondResetCardBatch records the audit entry and returns the batch result.
// It mirrors respondSubscriptionBatch but names the ids as reset-card ids: the
// shared helper would label them requested_subscription_ids, which in a log
// about cards points at the wrong entity.
func respondResetCardBatch(c *gin.Context, action string, result *model.SubscriptionBatchResult, ids []int) {
	params := map[string]any{
		"requested": result.Requested,
		"succeeded": result.Succeeded,
		"failed":    result.Failed,
	}
	if len(ids) > 0 {
		params["requested_reset_card_ids"] = append([]int{}, ids[:min(len(ids), subscriptionBatchAuditMaxIds)]...)
		if len(ids) > subscriptionBatchAuditMaxIds {
			params["requested_reset_card_ids_truncated"] = true
		}
	}
	recordManageAudit(c, action, params)
	common.ApiSuccess(c, result)
}

// AdminBatchInvalidateSubscriptionResetCards disables unused cards.
func AdminBatchInvalidateSubscriptionResetCards(c *gin.Context) {
	var req AdminBatchSubscriptionIdsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	result, err := model.BatchInvalidateSubscriptionResetCards(req.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	respondResetCardBatch(c, "subscription.reset_card_invalidate", result, req.Ids)
}

// AdminBatchDeleteSubscriptionResetCards deletes cards permanently.
func AdminBatchDeleteSubscriptionResetCards(c *gin.Context) {
	var req AdminBatchSubscriptionIdsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	result, err := model.BatchDeleteSubscriptionResetCards(req.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	respondResetCardBatch(c, "subscription.reset_card_delete", result, req.Ids)
}

// GetSelfSubscriptionResetCards lists the caller's own cards.
func GetSelfSubscriptionResetCards(c *gin.Context) {
	cards, err := model.ListUserSubscriptionResetCards(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, cards)
}

type RedeemSubscriptionResetCardRequest struct {
	CardId int    `json:"card_id"`
	Key    string `json:"key"`
}

// RedeemSubscriptionResetCard consumes one of the caller's cards. The card is
// located by id or by key; either way only the caller's own cards can match.
func RedeemSubscriptionResetCard(c *gin.Context) {
	var req RedeemSubscriptionResetCardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	result, err := model.RedeemSubscriptionResetCard(c.GetInt("id"), req.CardId, req.Key)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
