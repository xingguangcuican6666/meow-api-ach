package model

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
)

// A plan may restrict which models its subscription quota may be spent on. This
// is a separate concept from the extra quota limits in
// subscription_quota_limit.go:
//
//   - SubscriptionQuotaLimit.Models scopes one *additional* cap to some models,
//     while the plan's TotalAmount still applies to every model.
//   - SubscriptionPlan.AllowedModels is a *whitelist* for the whole plan. A
//     model outside it cannot use this subscription at all, so it never draws
//     down TotalAmount or any limit counter; the request is routed elsewhere
//     according to the user's billing preference.
//
// Both accept the same pattern syntax: an exact model name, or a single "*"
// wildcard at either end (claude-*, *-4o).
const subscriptionAllowedModelsMax = 256

// ErrSubscriptionModelNotAllowed means the user has an active subscription but
// none of them covers the requested model. The billing layer turns it into a
// distinct error code so routing can fall back to the wallet instead of
// reporting "quota insufficient".
var ErrSubscriptionModelNotAllowed = errors.New("subscription does not cover this model")

// ErrSubscriptionAllowedModelsInvalid is returned for admin input that cannot
// become a whitelist: too many entries, or entries that are all blank.
var ErrSubscriptionAllowedModelsInvalid = errors.New("subscription allowed model list is invalid")

// SubscriptionModelList is the plan whitelist stored as a JSON text column.
// An empty list means "every model", which keeps plans created before this
// feature working unchanged.
type SubscriptionModelList []string

func (l *SubscriptionModelList) Scan(value any) error {
	return scanJSONSliceColumn(value, (*[]string)(l))
}

func (l SubscriptionModelList) Value() (driver.Value, error) {
	return valueJSONSliceColumn(l)
}

// ValidateSubscriptionAllowedModels normalizes admin-provided whitelist input:
// entries are trimmed, blank and duplicate entries dropped, and the count is
// bounded. It returns nil for an input that carries no usable entry only when
// the caller passed nothing at all, so "clear the whitelist" and "you typed
// garbage" stay distinguishable.
func ValidateSubscriptionAllowedModels(models []string) ([]string, error) {
	if len(models) == 0 {
		return nil, nil
	}
	if len(models) > subscriptionAllowedModelsMax {
		return nil, ErrSubscriptionAllowedModelsInvalid
	}
	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
	}
	if len(normalized) == 0 {
		return nil, ErrSubscriptionAllowedModelsInvalid
	}
	return normalized, nil
}

// GetAllowedModels returns the whitelist in a read-safe shape. Read paths must
// not fail on a hand-edited or stale row, so blank entries are dropped here.
func (p *SubscriptionPlan) GetAllowedModels() []string {
	if p == nil || len(p.AllowedModels) == 0 {
		return nil
	}
	models := make([]string, 0, len(p.AllowedModels))
	for _, model := range p.AllowedModels {
		if model = strings.TrimSpace(model); model != "" {
			models = append(models, model)
		}
	}
	return models
}

// AllowsModel reports whether modelName may spend this plan's subscription
// quota. An empty whitelist allows every model. A request whose model name is
// unknown cannot be matched against a non-empty whitelist and is refused,
// matching how a model-scoped quota limit treats an empty model name.
func (p *SubscriptionPlan) AllowsModel(modelName string) bool {
	allowed := p.GetAllowedModels()
	if len(allowed) == 0 {
		return true
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return false
	}
	for _, pattern := range allowed {
		if matchSubscriptionModelPattern(pattern, modelName) {
			return true
		}
	}
	return false
}

// subscriptionModelNotAllowedError names the plan that refused the model. The
// message wraps ErrSubscriptionModelNotAllowed so the billing layer can match it
// with errors.Is while still telling the operator which plan and model.
func subscriptionModelNotAllowedError(plan *SubscriptionPlan, modelName string) error {
	title := ""
	if plan != nil {
		title = strings.TrimSpace(plan.Title)
	}
	if title == "" && plan != nil {
		title = fmt.Sprintf("#%d", plan.Id)
	}
	if title == "" {
		return fmt.Errorf("%w: %s", ErrSubscriptionModelNotAllowed, modelName)
	}
	return fmt.Errorf("%w: %s (plan %s)", ErrSubscriptionModelNotAllowed, modelName, title)
}
