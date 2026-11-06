package types

// SubscriptionLimitRef identifies one subscription quota-limit counter: the
// limit key configured on the plan, plus the model name when the limit is
// model-scoped. Pre-consume returns the refs a request was charged on so that
// settle and refund adjust exactly the same counters even if the plan was
// edited in between.
type SubscriptionLimitRef struct {
	Key   string `json:"key"`
	Model string `json:"model,omitempty"`
}
