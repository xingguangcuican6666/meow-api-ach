package model

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// A subscription plan may cap usage with several concurrent limits: a 5 hour
// window, a 7 day window, an independent quota for one model, and the legacy
// whole-period total, all at the same time. Every limit is described by the
// same SubscriptionQuotaLimit value; the window length and the model list are
// what make a limit a "5h" limit, a "7d" limit or a per-model limit.
//
// Window semantics are the ones operators expect from a rolling allowance: the
// window is anchored at the first request that charges it and the counter is
// zeroed once WindowSeconds elapsed since that anchor. Windows roll lazily at
// consume/settle/display time, so no background job is needed.
const (
	// SubscriptionLimitWindow5h is the 5 hour rolling window preset.
	SubscriptionLimitWindow5h = int64(5 * 60 * 60)
	// SubscriptionLimitWindow7d is the 7 day rolling window preset.
	SubscriptionLimitWindow7d = int64(7 * 24 * 60 * 60)
)

const (
	subscriptionQuotaLimitsMax      = 32
	subscriptionQuotaLimitModelsMax = 64
	subscriptionQuotaLimitKeyMax    = 64
	subscriptionQuotaLimitNameMax   = 64
)

var (
	ErrSubscriptionQuotaLimitsTooMany     = errors.New("subscription quota limits exceed the maximum count")
	ErrSubscriptionQuotaLimitKeyTooBig    = errors.New("subscription quota limit key is too long")
	ErrSubscriptionQuotaLimitDuplicateKey = errors.New("duplicate subscription quota limit key")
	ErrSubscriptionQuotaLimitAmount       = errors.New("subscription quota limit amount must be greater than 0")
	ErrSubscriptionQuotaLimitWindow       = errors.New("subscription quota limit window must not be negative")
	ErrSubscriptionQuotaLimitModels       = errors.New("subscription quota limit model list is invalid")
)

// SubscriptionQuotaLimit is one concurrent cap configured on a plan.
//
// Models empty means the limit applies to every model and keeps a single shared
// counter. A non-empty Models list restricts the limit to the matching models
// and keeps one independent counter per matched model, which is what "某模型独立
// 额度" needs. Model patterns are matched against the request's billing model
// name and support a single trailing or leading "*" wildcard.
type SubscriptionQuotaLimit struct {
	Key           string   `json:"key"`
	Name          string   `json:"name,omitempty"`
	WindowSeconds int64    `json:"window_seconds"`
	AmountTotal   int64    `json:"amount_total"`
	Models        []string `json:"models,omitempty"`
}

// SubscriptionLimitRef identifies the counter a request was charged on: the
// limit key plus the model name for per-model limits. Pre-consume persists the
// refs so settle and refund adjust exactly the counters they reserved, even if
// the plan was edited in between. The type lives in types/ because
// relay/common carries the refs on RelayInfo and model already depends on it.
type SubscriptionLimitRef = types.SubscriptionLimitRef

// SubscriptionQuotaLimitState is the persisted counter of one limit ref on one
// user subscription. WindowFrom is the anchor of the current rolling window; 0
// means the limit has no window (whole subscription lifetime).
type SubscriptionQuotaLimitState struct {
	Key        string `json:"key"`
	Model      string `json:"model,omitempty"`
	Used       int64  `json:"used"`
	WindowFrom int64  `json:"window_from,omitempty"`
}

// SubscriptionLimitUsage is the read model sent to the admin and user UIs.
type SubscriptionLimitUsage struct {
	Key           string   `json:"key"`
	Name          string   `json:"name,omitempty"`
	WindowSeconds int64    `json:"window_seconds"`
	AmountTotal   int64    `json:"amount_total"`
	Models        []string `json:"models,omitempty"`
	Model         string   `json:"model,omitempty"`
	AmountUsed    int64    `json:"amount_used"`
	AmountRemain  int64    `json:"amount_remain"`
	WindowFrom    int64    `json:"window_from,omitempty"`
	NextResetTime int64    `json:"next_reset_time,omitempty"`
}

// ---------------------------------------------------------------------------
// JSON text columns
// ---------------------------------------------------------------------------

type SubscriptionQuotaLimits []SubscriptionQuotaLimit

type SubscriptionQuotaLimitStates []SubscriptionQuotaLimitState

type SubscriptionLimitRefs []SubscriptionLimitRef

// scanJSONSliceColumn decodes a text/JSON column into a slice, accepting both
// driver return shapes ([]byte and string) like every other JSON column here.
func scanJSONSliceColumn[T any](value any, dst *[]T) error {
	bytesValue := jsonScanBytes(value)
	if len(bytesValue) == 0 {
		*dst = nil
		return nil
	}
	var decoded []T
	if err := common.Unmarshal(bytesValue, &decoded); err != nil {
		return err
	}
	*dst = decoded
	return nil
}

// valueJSONSliceColumn encodes a slice column. It returns string rather than
// []byte: under the PostgreSQL simple protocol []byte is encoded as bytea and
// writing it to a text/json column raises SQLSTATE 22P02.
func valueJSONSliceColumn[T any](src []T) (driver.Value, error) {
	if len(src) == 0 {
		return nil, nil
	}
	encoded, err := common.Marshal(src)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

func (l *SubscriptionQuotaLimits) Scan(value any) error {
	return scanJSONSliceColumn(value, (*[]SubscriptionQuotaLimit)(l))
}

func (l SubscriptionQuotaLimits) Value() (driver.Value, error) {
	return valueJSONSliceColumn(l)
}

func (s *SubscriptionQuotaLimitStates) Scan(value any) error {
	return scanJSONSliceColumn(value, (*[]SubscriptionQuotaLimitState)(s))
}

func (s SubscriptionQuotaLimitStates) Value() (driver.Value, error) {
	return valueJSONSliceColumn(s)
}

func (r *SubscriptionLimitRefs) Scan(value any) error {
	return scanJSONSliceColumn(value, (*[]SubscriptionLimitRef)(r))
}

func (r SubscriptionLimitRefs) Value() (driver.Value, error) {
	return valueJSONSliceColumn(r)
}

// ---------------------------------------------------------------------------
// Limit definition helpers
// ---------------------------------------------------------------------------

// ValidateSubscriptionQuotaLimits checks admin-provided limits and returns the
// normalized copy to persist. Keys are auto-assigned when empty so a limit
// keeps a stable identity for its counters.
func ValidateSubscriptionQuotaLimits(limits []SubscriptionQuotaLimit) ([]SubscriptionQuotaLimit, error) {
	if len(limits) > subscriptionQuotaLimitsMax {
		return nil, ErrSubscriptionQuotaLimitsTooMany
	}
	normalized := make([]SubscriptionQuotaLimit, 0, len(limits))
	seenKeys := make(map[string]struct{}, len(limits))
	for i, limit := range limits {
		limit.Key = strings.TrimSpace(limit.Key)
		if limit.Key == "" {
			limit.Key = fmt.Sprintf("limit-%d", i+1)
		}
		if len(limit.Key) > subscriptionQuotaLimitKeyMax {
			return nil, ErrSubscriptionQuotaLimitKeyTooBig
		}
		if _, ok := seenKeys[limit.Key]; ok {
			return nil, fmt.Errorf("%w: %s", ErrSubscriptionQuotaLimitDuplicateKey, limit.Key)
		}
		seenKeys[limit.Key] = struct{}{}
		limit.Name = strings.TrimSpace(limit.Name)
		if nameRunes := []rune(limit.Name); len(nameRunes) > subscriptionQuotaLimitNameMax {
			limit.Name = string(nameRunes[:subscriptionQuotaLimitNameMax])
		}
		if limit.WindowSeconds < 0 {
			return nil, ErrSubscriptionQuotaLimitWindow
		}
		if limit.AmountTotal <= 0 {
			return nil, ErrSubscriptionQuotaLimitAmount
		}
		// Bound the cap to the JavaScript-safe quota domain the rest of the
		// billing code uses, so a counter can neither exceed what the web UI
		// can render nor approach an int64 wrap when deltas accumulate.
		if limit.AmountTotal > common.MaxWalletQuota {
			return nil, ErrSubscriptionQuotaLimitAmount
		}
		if len(limit.Models) > subscriptionQuotaLimitModelsMax {
			return nil, ErrSubscriptionQuotaLimitModels
		}
		models := make([]string, 0, len(limit.Models))
		seenModels := make(map[string]struct{}, len(limit.Models))
		for _, pattern := range limit.Models {
			pattern = strings.TrimSpace(pattern)
			if pattern == "" {
				continue
			}
			if _, ok := seenModels[pattern]; ok {
				continue
			}
			seenModels[pattern] = struct{}{}
			models = append(models, pattern)
		}
		if len(models) == 0 && len(limit.Models) > 0 {
			return nil, ErrSubscriptionQuotaLimitModels
		}
		limit.Models = models
		normalized = append(normalized, limit)
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	return normalized, nil
}

// GetQuotaLimits returns the plan's limits in a defensive, read-safe shape:
// keys are filled in, unusable entries are dropped, and duplicate keys keep the
// first occurrence. Read paths must never fail on stale or hand-edited rows.
func (p *SubscriptionPlan) GetQuotaLimits() []SubscriptionQuotaLimit {
	if p == nil || len(p.QuotaLimits) == 0 {
		return nil
	}
	limits := make([]SubscriptionQuotaLimit, 0, len(p.QuotaLimits))
	seenKeys := make(map[string]struct{}, len(p.QuotaLimits))
	for i, limit := range p.QuotaLimits {
		limit.Key = strings.TrimSpace(limit.Key)
		if limit.Key == "" {
			limit.Key = fmt.Sprintf("limit-%d", i+1)
		}
		if _, ok := seenKeys[limit.Key]; ok {
			continue
		}
		seenKeys[limit.Key] = struct{}{}
		if limit.AmountTotal <= 0 || limit.WindowSeconds < 0 {
			continue
		}
		limits = append(limits, limit)
	}
	return limits
}

// limitMatchesModel reports whether a request for modelName is charged on the
// limit. A limit without models charges every request; a model-scoped limit
// charges only matching models, once per matched model name.
func limitMatchesModel(limit SubscriptionQuotaLimit, modelName string) bool {
	if len(limit.Models) == 0 {
		return true
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return false
	}
	for _, pattern := range limit.Models {
		if matchSubscriptionModelPattern(pattern, modelName) {
			return true
		}
	}
	return false
}

func matchSubscriptionModelPattern(pattern string, modelName string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(modelName, prefix)
	}
	if suffix, ok := strings.CutPrefix(pattern, "*"); ok {
		return strings.HasSuffix(modelName, suffix)
	}
	return pattern == modelName
}

// subscriptionLimitRefs lists the counters a request for modelName charges, in
// plan order. Model-scoped limits contribute one ref carrying the model name so
// each model keeps an independent quota.
func subscriptionLimitRefs(limits []SubscriptionQuotaLimit, modelName string) []SubscriptionLimitRef {
	modelName = strings.TrimSpace(modelName)
	if len(limits) == 0 {
		return nil
	}
	refs := make([]SubscriptionLimitRef, 0, len(limits))
	for _, limit := range limits {
		if len(limit.Models) == 0 {
			refs = append(refs, SubscriptionLimitRef{Key: limit.Key})
			continue
		}
		if modelName == "" || !limitMatchesModel(limit, modelName) {
			continue
		}
		refs = append(refs, SubscriptionLimitRef{Key: limit.Key, Model: modelName})
	}
	if len(refs) == 0 {
		return nil
	}
	return refs
}

func findSubscriptionLimit(limits []SubscriptionQuotaLimit, key string) (SubscriptionQuotaLimit, bool) {
	for _, limit := range limits {
		if limit.Key == key {
			return limit, true
		}
	}
	return SubscriptionQuotaLimit{}, false
}

// ---------------------------------------------------------------------------
// Limit state helpers
// ---------------------------------------------------------------------------

func findSubscriptionLimitState(states []SubscriptionQuotaLimitState, key string, modelName string) (SubscriptionQuotaLimitState, int) {
	for i, state := range states {
		if state.Key == key && state.Model == modelName {
			return state, i
		}
	}
	return SubscriptionQuotaLimitState{Key: key, Model: modelName}, -1
}

// rollSubscriptionLimitWindow returns the state as it applies at now, zeroing
// the counter when the rolling window elapsed. It is pure: callers decide
// whether the rolled value is persisted or only displayed.
func rollSubscriptionLimitWindow(state SubscriptionQuotaLimitState, limit SubscriptionQuotaLimit, now int64) SubscriptionQuotaLimitState {
	if limit.WindowSeconds <= 0 {
		state.WindowFrom = 0
		return state
	}
	if state.WindowFrom <= 0 {
		state.WindowFrom = now
		return state
	}
	if now >= state.WindowFrom+limit.WindowSeconds {
		state.WindowFrom = now
		state.Used = 0
	}
	return state
}

// rollSubscriptionLimitStates normalizes the persisted counters of sub against
// the plan's current limits: elapsed windows roll, and states whose limit was
// removed from the plan are dropped so the column stays bounded. It reports
// whether sub was modified and must be saved.
func rollSubscriptionLimitStates(sub *UserSubscription, plan *SubscriptionPlan, now int64) bool {
	if sub == nil {
		return false
	}
	limits := plan.GetQuotaLimits()
	if len(sub.QuotaLimitStates) == 0 {
		return false
	}
	dirty := false
	kept := make([]SubscriptionQuotaLimitState, 0, len(sub.QuotaLimitStates))
	for _, state := range sub.QuotaLimitStates {
		limit, ok := findSubscriptionLimit(limits, state.Key)
		if !ok {
			dirty = true
			continue
		}
		rolled := rollSubscriptionLimitWindow(state, limit, now)
		if rolled != state {
			dirty = true
		}
		kept = append(kept, rolled)
	}
	if dirty {
		sub.QuotaLimitStates = kept
	}
	return dirty
}

// upsertSubscriptionLimitState writes a rolled counter back onto the
// subscription, keeping the slice in place when the state already exists.
func upsertSubscriptionLimitState(sub *UserSubscription, state SubscriptionQuotaLimitState) {
	if _, index := findSubscriptionLimitState(sub.QuotaLimitStates, state.Key, state.Model); index >= 0 {
		sub.QuotaLimitStates[index] = state
		return
	}
	sub.QuotaLimitStates = append(sub.QuotaLimitStates, state)
}

// buildSubscriptionLimitUsage renders the counters of one subscription for the
// UIs. Windows are rolled in memory so the display never persists state, and
// model-scoped limits appear once per model that already has a counter.
func buildSubscriptionLimitUsage(plan *SubscriptionPlan, sub *UserSubscription, now int64) []SubscriptionLimitUsage {
	if plan == nil || sub == nil {
		return nil
	}
	limits := plan.GetQuotaLimits()
	if len(limits) == 0 {
		return nil
	}
	usage := make([]SubscriptionLimitUsage, 0, len(limits))
	for _, limit := range limits {
		if len(limit.Models) > 0 {
			emitted := false
			for _, state := range sub.QuotaLimitStates {
				if state.Key != limit.Key {
					continue
				}
				rolled := rollSubscriptionLimitWindow(state, limit, now)
				usage = append(usage, subscriptionLimitUsageOf(limit, rolled, now))
				emitted = true
			}
			// A model-scoped limit with no counter yet still advertises its cap.
			if !emitted {
				usage = append(usage, subscriptionLimitUsageOf(limit, SubscriptionQuotaLimitState{Key: limit.Key}, now))
			}
			continue
		}
		state, _ := findSubscriptionLimitState(sub.QuotaLimitStates, limit.Key, "")
		usage = append(usage, subscriptionLimitUsageOf(limit, rollSubscriptionLimitWindow(state, limit, now), now))
	}
	return usage
}

func subscriptionLimitUsageOf(limit SubscriptionQuotaLimit, state SubscriptionQuotaLimitState, now int64) SubscriptionLimitUsage {
	remain := limit.AmountTotal - state.Used
	if remain < 0 {
		remain = 0
	}
	var nextReset int64
	if limit.WindowSeconds > 0 && state.WindowFrom > 0 {
		nextReset = state.WindowFrom + limit.WindowSeconds
	}
	return SubscriptionLimitUsage{
		Key:           limit.Key,
		Name:          limit.Name,
		WindowSeconds: limit.WindowSeconds,
		AmountTotal:   limit.AmountTotal,
		Models:        limit.Models,
		Model:         state.Model,
		AmountUsed:    state.Used,
		AmountRemain:  remain,
		WindowFrom:    state.WindowFrom,
		NextResetTime: nextReset,
	}
}

// subscriptionLimitBlockedError names the limit that rejected a request so the
// caller can tell the operator which cap ran out. The message keeps the
// "subscription quota insufficient" prefix the billing layer matches on.
func subscriptionLimitBlockedError(limit SubscriptionQuotaLimit, state SubscriptionQuotaLimitState, amount int64) error {
	label := strings.TrimSpace(limit.Name)
	if label == "" {
		label = limit.Key
	}
	if state.Model != "" {
		label = fmt.Sprintf("%s (%s)", label, state.Model)
	}
	return fmt.Errorf("subscription quota insufficient, need=%d, limit %s remaining=%d",
		amount, label, max(limit.AmountTotal-state.Used, 0))
}

// checkSubscriptionLimits reports the first limit that has no room for amount,
// looking at the counters of sub as they apply at now. Windowed counters are
// rolled in memory here; the caller persists them before charging.
//
// The room test subtracts the used amount from the cap instead of adding the
// request to the used amount, so an oversized amount cannot wrap the int64 sum
// and silently pass the cap.
func checkSubscriptionLimits(sub *UserSubscription, plan *SubscriptionPlan, modelName string, amount int64, now int64) (SubscriptionQuotaLimit, SubscriptionQuotaLimitState, bool) {
	for _, limit := range plan.GetQuotaLimits() {
		if !limitMatchesModel(limit, modelName) {
			continue
		}
		refModel := ""
		if len(limit.Models) > 0 {
			refModel = strings.TrimSpace(modelName)
		}
		state, _ := findSubscriptionLimitState(sub.QuotaLimitStates, limit.Key, refModel)
		state = rollSubscriptionLimitWindow(state, limit, now)
		if amount > limit.AmountTotal-state.Used {
			return limit, state, true
		}
	}
	return SubscriptionQuotaLimit{}, SubscriptionQuotaLimitState{}, false
}

// adjustSubscriptionLimitCounters rolls the counters of sub to now and applies
// delta to every counter named by refs. A positive delta charges; a negative
// delta refunds, and counters never drop below zero. It reports whether sub
// changed and must be saved.
func adjustSubscriptionLimitCounters(sub *UserSubscription, plan *SubscriptionPlan, refs []SubscriptionLimitRef, delta int64, now int64) bool {
	if sub == nil || plan == nil {
		return false
	}
	changed := rollSubscriptionLimitStates(sub, plan, now)
	if delta == 0 || len(refs) == 0 {
		return changed
	}
	limits := plan.GetQuotaLimits()
	for _, ref := range refs {
		limit, ok := findSubscriptionLimit(limits, ref.Key)
		if !ok {
			continue
		}
		state, index := findSubscriptionLimitState(sub.QuotaLimitStates, ref.Key, ref.Model)
		// A refund for a counter that was already reaped has nothing to undo.
		if index < 0 && delta < 0 {
			continue
		}
		if limit.WindowSeconds > 0 && state.WindowFrom <= 0 {
			state.WindowFrom = now
		}
		state.Used = max(state.Used+delta, 0)
		upsertSubscriptionLimitState(sub, state)
		changed = true
	}
	return changed
}
