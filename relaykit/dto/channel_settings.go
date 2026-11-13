package dto

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/relaykit/types"
)

type ChannelSettings struct {
	TaskPluginKey             string `json:"task_plugin_key,omitempty"`
	ForceFormat               bool   `json:"force_format,omitempty"`
	ThinkingToContent         bool   `json:"thinking_to_content,omitempty"`
	Proxy                     string `json:"proxy"`
	PassThroughBodyEnabled    bool   `json:"pass_through_body_enabled,omitempty"`
	ResponsesWebSocketEnabled bool   `json:"responses_websocket_enabled,omitempty"`
	SystemPrompt              string `json:"system_prompt,omitempty"`
	SystemPromptOverride      bool   `json:"system_prompt_override,omitempty"`
	// TaskExtendPluginKeys lists the task plugins a New API channel (type 60)
	// is extended with. The upstream gateway may host many plugins, so the
	// channel serves every listed plugin's models while the request still pins
	// the executing plugin. TaskPluginKey remains the single type-61 binding
	// and stays valid on a New API channel as well.
	TaskExtendPluginKeys []string `json:"task_extend_plugin_keys,omitempty"`
	// HTTPProtocol controls outbound HTTP version negotiation for this channel.
	// Accepted values: "", "auto" (default), "http1".
	HTTPProtocol string `json:"http_protocol,omitempty"`
	// HTTP2ConnectionShards spreads HTTP/2 traffic across N independent transports
	// (1-8). Zero/unset means 1. Ignored when HTTPProtocol is "http1".
	HTTP2ConnectionShards int `json:"http2_connection_shards,omitempty"`
}

// BindsTaskPlugin reports whether the channel is bound to the task plugin,
// either through the single binding or the New API extension list.
func (s ChannelSettings) BindsTaskPlugin(key string) bool {
	if key == "" {
		return false
	}
	return s.TaskPluginKey == key || slices.Contains(s.TaskExtendPluginKeys, key)
}

// TaskPluginBindings returns the sorted, de-duplicated set of task plugins the
// channel binds through either field, so callers can compare bindings as a set.
func (s ChannelSettings) TaskPluginBindings() []string {
	bindings := make([]string, 0, len(s.TaskExtendPluginKeys)+1)
	if s.TaskPluginKey != "" {
		bindings = append(bindings, s.TaskPluginKey)
	}
	for _, key := range s.TaskExtendPluginKeys {
		if key != "" && !slices.Contains(bindings, key) {
			bindings = append(bindings, key)
		}
	}
	slices.Sort(bindings)
	return bindings
}

const (
	HTTPProtocolAuto         = "auto"
	HTTPProtocolHTTP1        = "http1"
	MaxHTTP2ConnectionShards = 8
)

// ValidateHTTPTransport validates save-time HTTP transport channel settings.
func (s *ChannelSettings) ValidateHTTPTransport() error {
	if s == nil {
		return nil
	}
	protocol := strings.ToLower(strings.TrimSpace(s.HTTPProtocol))
	switch protocol {
	case "", HTTPProtocolAuto, HTTPProtocolHTTP1:
	default:
		return fmt.Errorf("invalid http_protocol: %s", s.HTTPProtocol)
	}
	if s.HTTP2ConnectionShards < 0 || s.HTTP2ConnectionShards > MaxHTTP2ConnectionShards {
		return fmt.Errorf("invalid http2_connection_shards: %d", s.HTTP2ConnectionShards)
	}
	if protocol == HTTPProtocolHTTP1 && s.HTTP2ConnectionShards > 1 {
		return fmt.Errorf("http2_connection_shards must be 1 when http_protocol is http1")
	}
	return nil
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

const (
	BillingQueryTypeNewAPI         = "new_api"
	BillingQueryNewAPIUserSelfPath = "/api/user/self"
)

// BillingQueryConfig describes an optional channel-specific balance endpoint.
// A nil UseAPIKey keeps compatibility with configurations written before the
// authentication switch existed and means "use the channel API key".
type BillingQueryConfig struct {
	Type        string `json:"type,omitempty"`
	BaseURL     string `json:"base_url,omitempty"`
	BearerToken string `json:"bearer_token,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	UseAPIKey   *bool  `json:"use_api_key,omitempty"`
}

func (c *BillingQueryConfig) Validate() error {
	if c == nil {
		return nil
	}

	queryType := strings.ToLower(strings.TrimSpace(c.Type))
	if queryType == "" {
		return fmt.Errorf("billing query type is required")
	}
	if queryType != BillingQueryTypeNewAPI {
		return fmt.Errorf("unsupported billing query type: %s", c.Type)
	}

	baseURL := strings.TrimSpace(c.BaseURL)
	parsedURL, err := url.ParseRequestURI(baseURL)
	if err != nil || parsedURL.Hostname() == "" {
		return fmt.Errorf("billing query base URL must be an absolute HTTP or HTTPS URL")
	}
	if scheme := strings.ToLower(parsedURL.Scheme); scheme != "http" && scheme != "https" {
		return fmt.Errorf("billing query base URL must use HTTP or HTTPS")
	}
	if parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.ForceQuery || parsedURL.Fragment != "" ||
		strings.Contains(baseURL, "?") || strings.Contains(baseURL, "#") {
		return fmt.Errorf("billing query base URL must not contain credentials, query parameters, or fragments")
	}
	return nil
}

func (c *BillingQueryConfig) NormalizedBaseURL() string {
	if c == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
}

func (c *BillingQueryConfig) UsesAPIKey() bool {
	return c == nil || c.UseAPIKey == nil || *c.UseAPIKey
}

const (
	// RatioProbeSourceFollowAPI probes the channel relay Base URL (falling back to
	// the channel type default when the channel has no explicit Base URL).
	RatioProbeSourceFollowAPI = "follow_api"
	// RatioProbeSourceCustom probes an independent Base URL.
	RatioProbeSourceCustom = "custom"
	// RatioProbeDefaultPath is the New API group ratio endpoint.
	RatioProbeDefaultPath  = "/api/ratio_config"
	RatioProbeDefaultGroup = "default"

	RatioProbeStatusCompliant    = "compliant"
	RatioProbeStatusRejected     = "rejected"
	RatioProbeStatusError        = "error"
	RatioProbeStatusUnconfigured = "unconfigured"

	// MaxRatioProbeRatio bounds configured thresholds so a typo cannot turn into
	// an effectively unbounded comparison.
	MaxRatioProbeRatio = 1e6
	// MaxRatioProbeAuthorizationLength bounds the stored Authorization value so a
	// pasted blob cannot inflate the channel settings document.
	MaxRatioProbeAuthorizationLength = 8 << 10
	// ratioProbeEpsilon absorbs float round-trips through JSON so a threshold of
	// 1 still accepts an upstream ratio serialized as 0.9999999999999999.
	ratioProbeEpsilon = 1e-9
)

// RatioProbeConfig describes the periodic upstream group ratio probe. When the
// probe is enabled the scheduled task compares the upstream group ratio against
// the configured bounds and enables/disables the channel (or, for multi-key
// channels, the individual key) accordingly.
//
// A nil UseAPIKey means "send the channel API key as the Bearer token", which is
// the behaviour multi-key channels need: each key is probed with its own
// credential so keys can be judged independently. When UseAPIKey is false the
// probe sends Authorization instead, so one shared request covers every key.
type RatioProbeConfig struct {
	Enabled       bool     `json:"enabled,omitempty"`
	Source        string   `json:"source,omitempty"`
	BaseURL       string   `json:"base_url,omitempty"`
	Path          string   `json:"path,omitempty"`
	Group         string   `json:"group,omitempty"`
	MaxGroupRatio *float64 `json:"max_group_ratio,omitempty"`
	MinGroupRatio *float64 `json:"min_group_ratio,omitempty"`
	UseAPIKey     *bool    `json:"use_api_key,omitempty"`
	// Authorization is the complete header value (for example "Bearer xxx" or
	// "Basic xxx") sent when the probe does not use the channel API key. An empty
	// value sends no Authorization header at all.
	Authorization string `json:"authorization,omitempty"`

	// Probe state written by the scheduled task and rendered read-only in the
	// channel editor.
	LastProbeTime    int64    `json:"last_probe_time,omitempty"`
	LastGroupRatio   *float64 `json:"last_group_ratio,omitempty"`
	LastStatus       string   `json:"last_status,omitempty"`
	LastMessage      string   `json:"last_message,omitempty"`
	LastEnabledKeys  int      `json:"last_enabled_keys,omitempty"`
	LastDisabledKeys int      `json:"last_disabled_keys,omitempty"`
}

func (c *RatioProbeConfig) IsEnabled() bool {
	return c != nil && c.Enabled
}

func (c *RatioProbeConfig) NormalizedSource() string {
	if c == nil {
		return RatioProbeSourceFollowAPI
	}
	source := strings.ToLower(strings.TrimSpace(c.Source))
	if source == RatioProbeSourceCustom {
		return RatioProbeSourceCustom
	}
	return RatioProbeSourceFollowAPI
}

func (c *RatioProbeConfig) NormalizedBaseURL() string {
	if c == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
}

func (c *RatioProbeConfig) NormalizedPath() string {
	if c == nil {
		return RatioProbeDefaultPath
	}
	path := strings.TrimSpace(c.Path)
	if path == "" {
		return RatioProbeDefaultPath
	}
	return path
}

func (c *RatioProbeConfig) NormalizedGroup() string {
	if c == nil {
		return RatioProbeDefaultGroup
	}
	group := strings.TrimSpace(c.Group)
	if group == "" {
		return RatioProbeDefaultGroup
	}
	return group
}

func (c *RatioProbeConfig) UsesAPIKey() bool {
	return c == nil || c.UseAPIKey == nil || *c.UseAPIKey
}

// NormalizedAuthorization returns the configured Authorization header value. It
// is only sent when the probe does not use the channel API key, mirroring
// BillingQueryConfig.BearerToken.
func (c *RatioProbeConfig) NormalizedAuthorization() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Authorization)
}

// Accepts reports whether an upstream group ratio satisfies the configured
// bounds. Both bounds are inclusive.
func (c *RatioProbeConfig) Accepts(ratio float64) bool {
	if c == nil {
		return true
	}
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return false
	}
	if c.MaxGroupRatio != nil && ratio > *c.MaxGroupRatio+ratioProbeEpsilon {
		return false
	}
	if c.MinGroupRatio != nil && ratio < *c.MinGroupRatio-ratioProbeEpsilon {
		return false
	}
	return true
}

func (c *RatioProbeConfig) Validate() error {
	if !c.IsEnabled() {
		return nil
	}

	source := strings.ToLower(strings.TrimSpace(c.Source))
	switch source {
	case "", RatioProbeSourceFollowAPI:
	case RatioProbeSourceCustom:
		if err := ValidateRatioProbeBaseURL(c.NormalizedBaseURL()); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid ratio probe source: %s", c.Source)
	}

	path := c.NormalizedPath()
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return fmt.Errorf("ratio probe path must be an absolute path starting with /")
	}
	if strings.ContainsAny(path, "?#") {
		return fmt.Errorf("ratio probe path must not contain query parameters or fragments")
	}

	if c.MaxGroupRatio == nil && c.MinGroupRatio == nil {
		return fmt.Errorf("ratio probe requires a max or min group ratio")
	}
	if err := validateRatioProbeBound("max", c.MaxGroupRatio); err != nil {
		return err
	}
	if err := validateRatioProbeBound("min", c.MinGroupRatio); err != nil {
		return err
	}
	if c.MaxGroupRatio != nil && c.MinGroupRatio != nil && *c.MinGroupRatio > *c.MaxGroupRatio {
		return fmt.Errorf("ratio probe min group ratio must not exceed max group ratio")
	}
	return validateRatioProbeAuthorization(c.NormalizedAuthorization())
}

// validateRatioProbeAuthorization keeps a configured header value usable and
// harmless: bounded in length and free of control characters, so a saved channel
// setting can never inject extra request headers into the probe.
func validateRatioProbeAuthorization(authorization string) error {
	if authorization == "" {
		return nil
	}
	if len(authorization) > MaxRatioProbeAuthorizationLength {
		return fmt.Errorf("ratio probe authorization must not exceed %d characters", MaxRatioProbeAuthorizationLength)
	}
	for i := 0; i < len(authorization); i++ {
		if character := authorization[i]; character != '\t' && (character < 0x20 || character > 0x7e) {
			return fmt.Errorf("ratio probe authorization must contain printable ASCII characters only")
		}
	}
	return nil
}

// ValidateRatioProbeBaseURL rejects probe targets that cannot be used as an
// absolute upstream endpoint. It is shared by save-time validation and by the
// scheduled probe, which also accepts the channel relay Base URL.
func ValidateRatioProbeBaseURL(baseURL string) error {
	parsedURL, err := url.ParseRequestURI(baseURL)
	if err != nil || parsedURL.Hostname() == "" {
		return fmt.Errorf("ratio probe base URL must be an absolute HTTP or HTTPS URL")
	}
	if scheme := strings.ToLower(parsedURL.Scheme); scheme != "http" && scheme != "https" {
		return fmt.Errorf("ratio probe base URL must use HTTP or HTTPS")
	}
	if parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.ForceQuery || parsedURL.Fragment != "" ||
		strings.ContainsAny(baseURL, "?#") {
		return fmt.Errorf("ratio probe base URL must not contain credentials, query parameters, or fragments")
	}
	return nil
}

func validateRatioProbeBound(name string, bound *float64) error {
	if bound == nil {
		return nil
	}
	if math.IsNaN(*bound) || math.IsInf(*bound, 0) {
		return fmt.Errorf("ratio probe %s group ratio must be a finite number", name)
	}
	if *bound < 0 || *bound > MaxRatioProbeRatio {
		return fmt.Errorf("ratio probe %s group ratio must be between 0 and %g", name, MaxRatioProbeRatio)
	}
	return nil
}

const (
	// ImageUpscaleOnErrorFallback returns the original generated images when the
	// upscale call fails. It is the default policy.
	ImageUpscaleOnErrorFallback = "fallback"
	// ImageUpscaleOnErrorFail fails the whole image request when the upscale
	// call fails, even though the generation itself succeeded.
	ImageUpscaleOnErrorFail = "fail"

	defaultImageUpscaleTimeoutSeconds = 300
	maxImageUpscaleTimeoutSeconds     = 3600
	maxImageUpscaleModelLength        = 255
)

// ImageUpscaleConfig chains a second relay call after every successful
// image relay on this channel: the generated images are submitted to the
// target channel and model (typically a local upscaler exposed through an
// OpenAI Image Edits route) and the processed images replace the response body.
// The upscale call is billed separately under the target model's pricing.
type ImageUpscaleConfig struct {
	Enabled         bool   `json:"enabled"`
	TargetChannelID int    `json:"target_channel_id"`
	TargetModel     string `json:"target_model"`
	// Models optionally restricts the hook to these source model names. Empty
	// upscales every image request on the channel.
	Models []string `json:"models,omitempty"`
	// OnError controls the failure policy. Empty means ImageUpscaleOnErrorFallback.
	OnError string `json:"on_error,omitempty"`
	// TimeoutSeconds bounds the per-image upscale relay. Zero uses the default.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

func (c *ImageUpscaleConfig) NormalizedOnError() string {
	switch strings.ToLower(strings.TrimSpace(c.OnError)) {
	case ImageUpscaleOnErrorFail:
		return ImageUpscaleOnErrorFail
	default:
		return ImageUpscaleOnErrorFallback
	}
}

func (c *ImageUpscaleConfig) NormalizedTargetModel() string {
	return strings.TrimSpace(c.TargetModel)
}

// NormalizedModels trims the source model allowlist and drops empty entries.
// It returns nil when no restriction is configured.
func (c *ImageUpscaleConfig) NormalizedModels() []string {
	models := make([]string, 0, len(c.Models))
	for _, model := range c.Models {
		if model = strings.TrimSpace(model); model != "" {
			models = append(models, model)
		}
	}
	if len(models) == 0 {
		return nil
	}
	return models
}

// MatchesModel reports whether the source model should trigger the hook. An
// empty allowlist matches every model on the channel.
func (c *ImageUpscaleConfig) MatchesModel(model string) bool {
	models := c.NormalizedModels()
	if models == nil {
		return true
	}
	return slices.Contains(models, strings.TrimSpace(model))
}

func (c *ImageUpscaleConfig) EffectiveTimeoutSeconds() int {
	if c.TimeoutSeconds <= 0 {
		return defaultImageUpscaleTimeoutSeconds
	}
	return min(c.TimeoutSeconds, maxImageUpscaleTimeoutSeconds)
}

// Validate is the save-time check. A disabled config may stay incomplete so
// administrators can stage it; an enabled config must be actionable.
func (c *ImageUpscaleConfig) Validate() error {
	if c == nil || !c.Enabled {
		return nil
	}
	if c.TargetChannelID <= 0 {
		return fmt.Errorf("image_upscale.target_channel_id must be a positive channel id")
	}
	if c.NormalizedTargetModel() == "" {
		return fmt.Errorf("image_upscale.target_model is required")
	}
	if len(c.NormalizedTargetModel()) > maxImageUpscaleModelLength {
		return fmt.Errorf("image_upscale.target_model must not exceed %d characters", maxImageUpscaleModelLength)
	}
	switch strings.ToLower(strings.TrimSpace(c.OnError)) {
	case "", ImageUpscaleOnErrorFallback, ImageUpscaleOnErrorFail:
	default:
		return fmt.Errorf("image_upscale.on_error is invalid: %s", c.OnError)
	}
	for _, model := range c.NormalizedModels() {
		if len(model) > maxImageUpscaleModelLength {
			return fmt.Errorf("image_upscale.models entries must not exceed %d characters", maxImageUpscaleModelLength)
		}
	}
	if c.TimeoutSeconds < 0 || c.TimeoutSeconds > maxImageUpscaleTimeoutSeconds {
		return fmt.Errorf("image_upscale.timeout_seconds must be between 0 and %d", maxImageUpscaleTimeoutSeconds)
	}
	return nil
}

type ChannelOtherSettings struct {
	RuntimeAutomaticDisableOverrideEnabled bool   `json:"runtime_automatic_disable_override_enabled,omitempty"`
	RuntimeAutomaticDisableStatusCodes     string `json:"runtime_automatic_disable_status_codes,omitempty"`
	RuntimeAutomaticDisableKeywords        string `json:"runtime_automatic_disable_keywords,omitempty"`
	// EmptyResponseRetryOverrideEnabled makes EmptyResponseRetryEnabled below win
	// over the global switch for this channel.
	EmptyResponseRetryOverrideEnabled bool `json:"empty_response_retry_override_enabled,omitempty"`
	EmptyResponseRetryEnabled         bool `json:"empty_response_retry_enabled,omitempty"`
	// EmptyResponseRetryInPlace keeps the retry on this channel instead of moving
	// to the next candidate.
	EmptyResponseRetryInPlace bool `json:"empty_response_retry_in_place,omitempty"`
	// AutomaticRetryOverrideEnabled enables channel-specific forced retry rules.
	AutomaticRetryOverrideEnabled bool `json:"automatic_retry_override_enabled,omitempty"`
	// AutomaticRetryStatusCodes contains comma-separated HTTP status codes/ranges.
	AutomaticRetryStatusCodes string `json:"automatic_retry_status_codes,omitempty"`
	// AutomaticRetryKeywords contains one case-insensitive keyword per line.
	AutomaticRetryKeywords string `json:"automatic_retry_keywords,omitempty"`
	// Deprecated names retained so settings saved by the previous implementation remain readable.
	AutomaticDisableOverrideEnabled       bool                  `json:"automatic_disable_override_enabled,omitempty"`
	AutomaticDisableStatusCodes           string                `json:"automatic_disable_status_codes,omitempty"`
	AutomaticDisableKeywords              string                `json:"automatic_disable_keywords,omitempty"`
	AzureResponsesVersion                 string                `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType         `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                  *bool                 `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool                  `json:"claude_beta_query,omitempty"`          // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                      bool                  `json:"allow_service_tier,omitempty"`         // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool                  `json:"allow_inference_geo,omitempty"`        // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool                  `json:"allow_speed,omitempty"`                // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool                  `json:"allow_safety_identifier,omitempty"`    // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool                  `json:"disable_store,omitempty"`              // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool                  `json:"allow_include_obfuscation,omitempty"`  // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	DisableTaskPollingSleep               bool                  `json:"disable_task_polling_sleep,omitempty"` // 是否跳过异步任务轮询间隔
	AwsKeyType                            AwsKeyType            `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool                  `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool                  `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64                 `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string              `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string              `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string              `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型
	AdvancedCustom                        *AdvancedCustomConfig `json:"advanced_custom,omitempty"`
	BillingQuery                          *BillingQueryConfig   `json:"billing_query,omitempty"`
	RatioProbe                            *RatioProbeConfig     `json:"ratio_probe,omitempty"`
	ImageUpscale                          *ImageUpscaleConfig   `json:"image_upscale,omitempty"`
	// OllamaOpenAIChat routes Ollama chat completions to the OpenAI-compatible
	// /v1/chat/completions endpoint. When unset, chat completions keep using
	// the native /api/chat protocol.
	OllamaOpenAIChat bool `json:"ollama_openai_chat,omitempty"`
	// ToolLossPolicy is a channel-level opt-in for request-phase conversion
	// rejection. Empty follows the default allow policy. Accepted values:
	// "", "allow", "safe", "strict".
	ToolLossPolicy string `json:"tool_loss_policy,omitempty"`
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}

// ValidateToolLossPolicy validates the channel-level request-phase tool-loss
// policy. Empty keeps the default allow policy.
func (s *ChannelOtherSettings) ValidateToolLossPolicy() error {
	if s == nil {
		return nil
	}
	switch strings.TrimSpace(s.ToolLossPolicy) {
	case "", string(types.ConversionLossPolicyAllow), string(types.ConversionLossPolicySafe), string(types.ConversionLossPolicyStrict):
		return nil
	default:
		return fmt.Errorf("invalid tool_loss_policy: %s", s.ToolLossPolicy)
	}
}

const (
	AdvancedCustomConverterSGLangRerank                   = "jina_rerank_to_sglang"
	advancedCustomConverterNone                           = "none"
	advancedCustomConverterClaudeMessagesToOpenAIChat     = "anthropic_messages_to_openai_chat_completions"
	advancedCustomConverterClaudeMessagesToOpenAIChatPure = "anthropic_messages_to_openai_chat_completions_pure"
	advancedCustomConverterOpenAIChatToClaudeMessages     = "openai_chat_completions_to_anthropic_messages"
	advancedCustomConverterOpenAIChatToOpenAIResponses    = "openai_chat_completions_to_openai_responses"
	advancedCustomConverterOpenAIResponsesToOpenAIChat    = "openai_responses_to_openai_chat_completions"
	advancedCustomConverterOpenAIResponsesToGemini        = "openai_responses_to_gemini_generate_content"
	advancedCustomConverterGeminiContentToOpenAIChat      = "gemini_generate_content_to_openai_chat_completions"
	advancedCustomConverterOpenAIChatToGeminiContent      = "openai_chat_completions_to_gemini_generate_content"
)

const (
	AdvancedCustomAuthTypeNone   = "none"
	AdvancedCustomAuthTypeHeader = "header"
	AdvancedCustomAuthTypeQuery  = "query"
)

type AdvancedCustomConfig struct {
	Routes []AdvancedCustomRoute `json:"advanced_routes,omitempty"`
}

type AdvancedCustomRoute struct {
	IncomingPath           string                   `json:"incoming_path,omitempty"`
	UpstreamPath           string                   `json:"upstream_path,omitempty"`
	Converter              string                   `json:"converter,omitempty"`
	Models                 []string                 `json:"models,omitempty"`
	Auth                   *AdvancedCustomRouteAuth `json:"auth,omitempty"`
	PassThroughBodyEnabled bool                     `json:"pass_through_body_enabled,omitempty"`
}

// SupportsPassThroughBody reports whether the route converter leaves the request body untouched.
func (r AdvancedCustomRoute) SupportsPassThroughBody() bool {
	switch strings.TrimSpace(r.Converter) {
	case "", advancedCustomConverterNone, AdvancedCustomConverterSGLangRerank:
		return true
	default:
		return false
	}
}

type AdvancedCustomRouteAuth struct {
	Type  string `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

const (
	advancedCustomModelPlaceholder = "{model}"
	advancedCustomModelRegexPrefix = "re:"
)

const (
	advancedCustomEndpointPathOpenAIChat             = "/v1/chat/completions"
	advancedCustomEndpointPathOpenAIResponses        = "/v1/responses"
	advancedCustomEndpointPathOpenAIResponsesCompact = "/v1/responses/compact"
	advancedCustomEndpointPathOpenAIAlphaSearch      = "/v1/alpha/search"
	advancedCustomEndpointPathClaudeMessages         = "/v1/messages"
	advancedCustomEndpointPathJinaRerank             = "/v1/rerank"
	advancedCustomEndpointPathImageGeneration        = "/v1/images/generations"
	advancedCustomEndpointPathEmbeddings             = "/v1/embeddings"
)

const (
	// AdvancedCustomModelListPath identifies the optional OpenAI Models discovery route.
	AdvancedCustomModelListPath = "/v1/models"
	// AdvancedCustomBalancePath identifies the optional balance lookup route used by channel management.
	AdvancedCustomBalancePath = "/v1/dashboard/billing/credit_grants"
)

// IsNative reports whether the route forwards requests without protocol
// conversion. Validate normalizes an empty converter to none, but callers may
// see configurations that were never saved.
func (r AdvancedCustomRoute) IsNative() bool {
	converter := strings.TrimSpace(r.Converter)
	return converter == "" || converter == advancedCustomConverterNone
}

// MatchPath returns the first route whose IncomingPath matches requestPath.
// Matching mirrors the relay adaptor: exact match, {model} placeholder, and
// :generateContent <-> :streamGenerateContent equivalence.
func (c *AdvancedCustomConfig) MatchPath(requestPath string) (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	for _, route := range c.Routes {
		if matchAdvancedCustomIncomingPath(strings.TrimSpace(route.IncomingPath), requestPath) {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// MatchPathForModel returns the first route whose IncomingPath and Models match.
// An empty Models list is a catch-all fallback for that incoming path.
func (c *AdvancedCustomConfig) MatchPathForModel(requestPath string, model string) (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	model = strings.TrimSpace(model)
	for _, route := range c.Routes {
		if matchAdvancedCustomIncomingPath(strings.TrimSpace(route.IncomingPath), requestPath) &&
			matchAdvancedCustomRouteModel(route.Models, model) {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// ModelListRoute returns the explicitly configured OpenAI Models discovery route.
// Template routes that merely happen to match /v1/models are not discovery routes.
func (c *AdvancedCustomConfig) ModelListRoute() (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	for _, route := range c.Routes {
		if strings.TrimSpace(route.IncomingPath) == AdvancedCustomModelListPath {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// BalanceRoute returns the explicitly configured channel-management balance route.
func (c *AdvancedCustomConfig) BalanceRoute() (AdvancedCustomRoute, bool) {
	if c == nil {
		return AdvancedCustomRoute{}, false
	}
	for _, route := range c.Routes {
		if strings.TrimSpace(route.IncomingPath) == AdvancedCustomBalancePath {
			return route, true
		}
	}
	return AdvancedCustomRoute{}, false
}

// SupportsPath reports whether any route matches requestPath.
func (c *AdvancedCustomConfig) SupportsPath(requestPath string) bool {
	_, ok := c.MatchPath(requestPath)
	return ok
}

// SupportsPathForModel reports whether any route matches requestPath and model.
func (c *AdvancedCustomConfig) SupportsPathForModel(requestPath string, model string) bool {
	_, ok := c.MatchPathForModel(requestPath, model)
	return ok
}

func (c *AdvancedCustomConfig) SupportedEndpointTypesForModel(model string) []types.EndpointType {
	if c == nil {
		return nil
	}
	model = strings.TrimSpace(model)
	endpoints := make([]types.EndpointType, 0, len(c.Routes))
	seen := make(map[types.EndpointType]struct{}, len(c.Routes))
	for _, route := range c.Routes {
		if !matchAdvancedCustomRouteModel(route.Models, model) {
			continue
		}
		endpointType, ok := advancedCustomEndpointTypeFromIncomingPath(strings.TrimSpace(route.IncomingPath))
		if !ok {
			continue
		}
		if _, exists := seen[endpointType]; exists {
			continue
		}
		seen[endpointType] = struct{}{}
		endpoints = append(endpoints, endpointType)
	}
	return endpoints
}

func advancedCustomEndpointTypeFromIncomingPath(incomingPath string) (types.EndpointType, bool) {
	switch incomingPath {
	case advancedCustomEndpointPathOpenAIChat:
		return types.EndpointTypeOpenAI, true
	case advancedCustomEndpointPathOpenAIResponses:
		return types.EndpointTypeOpenAIResponse, true
	case advancedCustomEndpointPathOpenAIResponsesCompact:
		return types.EndpointTypeOpenAIResponseCompact, true
	case advancedCustomEndpointPathOpenAIAlphaSearch:
		return types.EndpointTypeOpenAIAlphaSearch, true
	case advancedCustomEndpointPathClaudeMessages:
		return types.EndpointTypeAnthropic, true
	case advancedCustomEndpointPathJinaRerank:
		return types.EndpointTypeJinaRerank, true
	case advancedCustomEndpointPathImageGeneration:
		return types.EndpointTypeImageGeneration, true
	case advancedCustomEndpointPathEmbeddings:
		return types.EndpointTypeEmbeddings, true
	default:
		if isAdvancedCustomGeminiIncomingPath(incomingPath) {
			return types.EndpointTypeGemini, true
		}
		return "", false
	}
}

func isAdvancedCustomGeminiIncomingPath(incomingPath string) bool {
	if !strings.HasPrefix(incomingPath, "/v1beta/models/") {
		return false
	}
	return strings.Contains(incomingPath, ":generateContent") || strings.Contains(incomingPath, ":streamGenerateContent")
}

func matchAdvancedCustomRouteModel(models []string, model string) bool {
	normalizedModels := normalizeAdvancedCustomRouteModels(models)
	if len(normalizedModels) == 0 {
		return true
	}
	for _, allowedModel := range normalizedModels {
		if matchAdvancedCustomRouteModelRule(allowedModel, model) {
			return true
		}
	}
	return false
}

// advancedCustomModelRegexCache caches compiled route model patterns. Route model
// matching runs on the request hot path (distributor affinity, ability filtering,
// channel cache filtering, adaptor resolve), so patterns must not be recompiled per
// request. Invalid patterns are cached as nil to avoid recompiling them as well.
var advancedCustomModelRegexCache sync.Map // pattern string -> *regexp.Regexp (nil when invalid)

func compileAdvancedCustomModelRegex(pattern string) *regexp.Regexp {
	if cached, ok := advancedCustomModelRegexCache.Load(pattern); ok {
		re, _ := cached.(*regexp.Regexp)
		return re
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}
	advancedCustomModelRegexCache.Store(pattern, re)
	return re
}

func matchAdvancedCustomRouteModelRule(rule string, model string) bool {
	if !strings.HasPrefix(rule, advancedCustomModelRegexPrefix) {
		return rule == model
	}
	pattern := strings.TrimPrefix(rule, advancedCustomModelRegexPrefix)
	if pattern == "" {
		return false
	}
	re := compileAdvancedCustomModelRegex(pattern)
	return re != nil && re.MatchString(model)
}

func matchAdvancedCustomIncomingPath(configuredPath string, requestPath string) bool {
	if matchAdvancedCustomIncomingPathTemplate(configuredPath, requestPath) {
		return true
	}
	if strings.Contains(configuredPath, ":generateContent") {
		streamPath := strings.Replace(configuredPath, ":generateContent", ":streamGenerateContent", 1)
		return matchAdvancedCustomIncomingPathTemplate(streamPath, requestPath)
	}
	return false
}

func matchAdvancedCustomIncomingPathTemplate(configuredPath string, requestPath string) bool {
	if !strings.Contains(configuredPath, advancedCustomModelPlaceholder) {
		return configuredPath == requestPath
	}

	parts := strings.Split(configuredPath, advancedCustomModelPlaceholder)
	if len(parts) != 2 {
		return false
	}
	if !strings.HasPrefix(requestPath, parts[0]) || !strings.HasSuffix(requestPath, parts[1]) {
		return false
	}

	model := strings.TrimSuffix(strings.TrimPrefix(requestPath, parts[0]), parts[1])
	return model != "" && !strings.Contains(model, "/")
}

func IsAdvancedCustomConverterAllowed(converter string) bool {
	switch converter {
	case advancedCustomConverterNone,
		AdvancedCustomConverterSGLangRerank,
		advancedCustomConverterClaudeMessagesToOpenAIChat,
		advancedCustomConverterClaudeMessagesToOpenAIChatPure,
		advancedCustomConverterOpenAIChatToClaudeMessages,
		advancedCustomConverterOpenAIChatToOpenAIResponses,
		advancedCustomConverterOpenAIResponsesToOpenAIChat,
		advancedCustomConverterOpenAIResponsesToGemini,
		advancedCustomConverterGeminiContentToOpenAIChat,
		advancedCustomConverterOpenAIChatToGeminiContent:
		return true
	default:
		return false
	}
}

func (c *AdvancedCustomConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("advanced_custom is required")
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("advanced_custom requires at least one route")
	}

	paths := make(map[string]*advancedCustomPathModelState, len(c.Routes))
	modelListRouteIndex := -1
	balanceRouteIndex := -1
	for i := range c.Routes {
		route := c.Routes[i]
		route.IncomingPath = strings.TrimSpace(route.IncomingPath)
		upstreamPath := strings.TrimSpace(route.UpstreamPath)
		route.Converter = strings.TrimSpace(route.Converter)
		if route.Converter == "" {
			route.Converter = advancedCustomConverterNone
		}

		if route.IncomingPath == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path is required", i)
		}
		if !strings.HasPrefix(route.IncomingPath, "/") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must start with /", i)
		}
		if strings.Contains(route.IncomingPath, "?") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].incoming_path must not include query", i)
		}
		if route.IncomingPath == AdvancedCustomModelListPath || route.IncomingPath == AdvancedCustomBalancePath {
			managementRouteName := route.IncomingPath
			previousIndex := modelListRouteIndex
			if route.IncomingPath == AdvancedCustomBalancePath {
				previousIndex = balanceRouteIndex
			}
			if previousIndex >= 0 {
				return fmt.Errorf("advanced_custom.advanced_routes[%d] duplicates the %s route at advanced_routes[%d]", i, managementRouteName, previousIndex)
			}
			if route.IncomingPath == AdvancedCustomModelListPath {
				modelListRouteIndex = i
			} else {
				balanceRouteIndex = i
			}
			if len(normalizeAdvancedCustomRouteModels(route.Models)) > 0 {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].models must be empty for %s", i, managementRouteName)
			}
			if route.Converter != advancedCustomConverterNone {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].converter must be none for %s", i, managementRouteName)
			}
			if strings.Contains(upstreamPath, advancedCustomModelPlaceholder) {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must not contain %s for %s", i, advancedCustomModelPlaceholder, managementRouteName)
			}
			if route.PassThroughBodyEnabled {
				return fmt.Errorf("advanced_custom.advanced_routes[%d].pass_through_body_enabled must be false for %s", i, managementRouteName)
			}
		}
		if err := validateAdvancedCustomRouteModels(i, route.IncomingPath, route.Models, paths); err != nil {
			return err
		}

		if upstreamPath == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path is required", i)
		}
		if err := validateAdvancedCustomUpstreamTarget(i, upstreamPath); err != nil {
			return err
		}

		if !IsAdvancedCustomConverterAllowed(route.Converter) {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].converter is not registered: %s", i, route.Converter)
		}
		if err := validateAdvancedCustomConverterPath(i, route.IncomingPath, route.Converter); err != nil {
			return err
		}
		if route.PassThroughBodyEnabled && !route.SupportsPassThroughBody() {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].pass_through_body_enabled requires converter none: %s", i, route.Converter)
		}
		if err := validateAdvancedCustomRouteAuth(i, route.Auth); err != nil {
			return err
		}
	}

	return nil
}

type advancedCustomPathModelState struct {
	catchAllIndex int
	modelIndexes  map[string]int
}

func validateAdvancedCustomRouteModels(index int, incomingPath string, models []string, paths map[string]*advancedCustomPathModelState) error {
	state := paths[incomingPath]
	if state == nil {
		state = &advancedCustomPathModelState{
			catchAllIndex: -1,
			modelIndexes:  make(map[string]int),
		}
		paths[incomingPath] = state
	}

	normalizedModels := normalizeAdvancedCustomRouteModels(models)
	if len(normalizedModels) == 0 {
		if state.catchAllIndex >= 0 {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].models catch-all already exists for incoming_path: %s", index, incomingPath)
		}
		state.catchAllIndex = index
		return nil
	}

	if state.catchAllIndex >= 0 {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].models catch-all route must be last for incoming_path: %s", index, incomingPath)
	}

	seenInRoute := make(map[string]struct{}, len(normalizedModels))
	for _, model := range normalizedModels {
		if err := validateAdvancedCustomRouteModelRule(index, incomingPath, model); err != nil {
			return err
		}
		if _, exists := seenInRoute[model]; exists {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].models contains duplicate model for incoming_path %s: %s", index, incomingPath, model)
		}
		seenInRoute[model] = struct{}{}
		if existingIndex, exists := state.modelIndexes[model]; exists {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].models overlaps with advanced_routes[%d] for incoming_path %s: %s", index, existingIndex, incomingPath, model)
		}
		state.modelIndexes[model] = index
	}
	return nil
}

func validateAdvancedCustomRouteModelRule(index int, incomingPath string, model string) error {
	if !strings.HasPrefix(model, advancedCustomModelRegexPrefix) {
		return nil
	}
	pattern := strings.TrimPrefix(model, advancedCustomModelRegexPrefix)
	if pattern == "" {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].models regex is empty for incoming_path %s: %s", index, incomingPath, model)
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].models regex is invalid for incoming_path %s: %s", index, incomingPath, model)
	}
	return nil
}

func normalizeAdvancedCustomRouteModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model != "" {
			normalized = append(normalized, model)
		}
	}
	return normalized
}

func validateAdvancedCustomUpstreamTarget(index int, upstreamPath string) error {
	if strings.HasPrefix(upstreamPath, "/") {
		if strings.HasPrefix(upstreamPath, "//") {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must be a full URL or a path starting with /", index)
		}
		return nil
	}

	parsedURL, err := url.Parse(upstreamPath)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must be a full URL or a path starting with /", index)
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return fmt.Errorf("advanced_custom.advanced_routes[%d].upstream_path must use http or https", index)
	}
	return nil
}

func validateAdvancedCustomConverterPath(index int, incomingPath string, converter string) error {
	if converter == AdvancedCustomConverterSGLangRerank && (incomingPath == "/v1/rerank" || incomingPath == "/rerank") {
		return nil
	}
	if incomingPath == advancedCustomEndpointPathOpenAIAlphaSearch {
		if converter == advancedCustomConverterNone {
			return nil
		}
		return fmt.Errorf("advanced_custom.advanced_routes[%d].converter does not match incoming_path: %s", index, converter)
	}
	switch converter {
	case advancedCustomConverterNone:
		return nil
	case advancedCustomConverterClaudeMessagesToOpenAIChat,
		advancedCustomConverterClaudeMessagesToOpenAIChatPure:
		if incomingPath == "/v1/messages" {
			return nil
		}
	case advancedCustomConverterOpenAIChatToClaudeMessages,
		advancedCustomConverterOpenAIChatToOpenAIResponses,
		advancedCustomConverterOpenAIChatToGeminiContent:
		if incomingPath == "/v1/chat/completions" {
			return nil
		}
	case advancedCustomConverterOpenAIResponsesToOpenAIChat:
		if incomingPath == "/v1/responses" {
			return nil
		}
	case advancedCustomConverterOpenAIResponsesToGemini:
		if incomingPath == "/v1/responses" {
			return nil
		}
	case advancedCustomConverterGeminiContentToOpenAIChat:
		if strings.Contains(incomingPath, ":generateContent") || strings.Contains(incomingPath, ":streamGenerateContent") {
			return nil
		}
	}
	return fmt.Errorf("advanced_custom.advanced_routes[%d].converter does not match incoming_path: %s", index, converter)
}

func validateAdvancedCustomRouteAuth(index int, auth *AdvancedCustomRouteAuth) error {
	if auth == nil {
		return nil
	}
	authType := strings.TrimSpace(auth.Type)
	switch authType {
	case AdvancedCustomAuthTypeNone:
		return nil
	case AdvancedCustomAuthTypeHeader, AdvancedCustomAuthTypeQuery:
		if strings.TrimSpace(auth.Name) == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.name is required", index)
		}
		if strings.TrimSpace(auth.Value) == "" {
			return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.value is required", index)
		}
		return nil
	default:
		return fmt.Errorf("advanced_custom.advanced_routes[%d].auth.type is invalid: %s", index, auth.Type)
	}
}
