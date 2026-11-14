package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

func MidjourneyErrorWrapper(code int, desc string) *taskdto.MidjourneyResponse {
	return &taskdto.MidjourneyResponse{
		Code:        code,
		Description: desc,
	}
}

func MidjourneyErrorWithStatusCodeWrapper(code int, desc string, statusCode int) *taskdto.MidjourneyResponseWithStatusCode {
	return &taskdto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   *MidjourneyErrorWrapper(code, desc),
	}
}

//// OpenAIErrorWrapper wraps an error into an OpenAIErrorWithStatusCode
//func OpenAIErrorWrapper(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	text := err.Error()
//	lowerText := strings.ToLower(text)
//	if !strings.HasPrefix(lowerText, "get file base64 from url") && !strings.HasPrefix(lowerText, "mime type is not supported") {
//		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
//			common.SysLog(fmt.Sprintf("error: %s", text))
//			text = "请求上游地址失败"
//		}
//	}
//	openAIError := dto.OpenAIError{
//		Message: text,
//		Type:    "new_api_error",
//		Code:    code,
//	}
//	return &dto.OpenAIErrorWithStatusCode{
//		Error:      openAIError,
//		StatusCode: statusCode,
//	}
//}
//
//func OpenAIErrorWrapperLocal(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	openaiErr := OpenAIErrorWrapper(err, code, statusCode)
//	openaiErr.LocalError = true
//	return openaiErr
//}

func ClaudeErrorWrapper(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	text := err.Error()
	lowerText := strings.ToLower(text)
	if !strings.HasPrefix(lowerText, "get file base64 from url") {
		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
			common.SysLog(fmt.Sprintf("error: %s", text))
			text = "请求上游地址失败"
		}
	}
	claudeError := types.ClaudeError{
		Message: text,
		Type:    "new_api_error",
	}
	return &dto.ClaudeErrorWithStatusCode{
		Error:      claudeError,
		StatusCode: statusCode,
	}
}

func ClaudeErrorWrapperLocal(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	claudeErr := ClaudeErrorWrapper(err, code, statusCode)
	claudeErr.LocalError = true
	return claudeErr
}

// sanitizedUpstreamErrorMessage is the fallback client message used when no
// status-code-specific phrasing applies. Single source, so the wording (and
// its language) can be changed in one place.
const sanitizedUpstreamErrorMessage = "The upstream request failed. Please contact the service administrator with the request ID."

// contentSafetyClientMessage replaces content-policy refusals whose verbatim
// text names the upstream provider (e.g. "request blocked by Gemini API: ...").
// The client still learns the content was rejected — an actionable signal —
// without being told which provider rejected it.
const contentSafetyClientMessage = "Your request was rejected by the content safety policy. Please modify your input."

// standardUpstreamStatusMessages maps a final HTTP status code to a fixed,
// sensitive-data-free client message. Status codes absent here fall back to
// sanitizedUpstreamErrorMessage.
var standardUpstreamStatusMessages = map[int]string{
	http.StatusUnauthorized:          "The upstream provider denied access (account or quota issue). Please retry later or contact the service administrator.",
	http.StatusForbidden:             "The upstream provider denied access (account or quota issue). Please retry later or contact the service administrator.",
	http.StatusPaymentRequired:       "The upstream account has insufficient balance. Please contact the service administrator.",
	http.StatusNotFound:              "The requested upstream model or resource is unavailable.",
	http.StatusRequestTimeout:        "The upstream provider timed out. Please retry.",
	http.StatusGatewayTimeout:        "The upstream provider timed out. Please retry.",
	http.StatusRequestEntityTooLarge: "The request was too large for the upstream provider.",
	http.StatusTooManyRequests:       "The upstream provider is rate limiting or out of quota. Please retry later.",
	http.StatusInternalServerError:   "The upstream service is temporarily unavailable. Please retry, or contact the service administrator with the request ID.",
	http.StatusBadGateway:            "The upstream service is temporarily unavailable. Please retry, or contact the service administrator with the request ID.",
	http.StatusServiceUnavailable:    "The upstream service is temporarily unavailable. Please retry, or contact the service administrator with the request ID.",
}

// localActionableErrorCodes are error codes this service authors itself and
// whose message the client must read verbatim to act on (fix the request, top
// up their own wallet, pick another model, retry another channel). They are
// never replaced by the standardized upstream phrasing. Upstream provider codes
// are a dynamic open set, so the design is an allowlist of local codes rather
// than a denylist of upstream ones.
var localActionableErrorCodes = map[types.ErrorCode]bool{
	types.ErrorCodeInvalidRequest:             true,
	types.ErrorCodeSensitiveWordsDetected:     true,
	types.ErrorCodeCountTokenFailed:           true,
	types.ErrorCodeModelPriceError:            true,
	types.ErrorCodeInvalidApiType:             true,
	types.ErrorCodeGetChannelFailed:           true,
	types.ErrorCodeGenRelayInfoFailed:         true,
	types.ErrorCodeReadRequestBodyFailed:      true,
	types.ErrorCodeConvertRequestFailed:       true,
	types.ErrorCodeAccessDenied:               true,
	types.ErrorCodeBadRequestBody:             true,
	types.ErrorCodeInsufficientUserQuota:      true,
	types.ErrorCodePreConsumeTokenQuotaFailed: true,
	types.ErrorCodeQueryDataError:             true,
	types.ErrorCodeUpdateDataError:            true,
	types.ErrorCodeModelNotFound:              true,
	types.ErrorCodeJsonMarshalFailed:          true,
}

// isLocalActionableErrorCode reports whether code was authored by this service
// and must reach the client verbatim. All channel-selection/config codes (the
// "channel:" prefix) qualify as well.
func isLocalActionableErrorCode(code types.ErrorCode) bool {
	if strings.HasPrefix(string(code), "channel:") {
		return true
	}
	return localActionableErrorCodes[code]
}

// isContentSafetyErrorCode reports whether code marks a content-policy refusal
// whose verbatim text embeds the upstream provider name. These get a dedicated
// provider-free message rather than the generic status-code phrasing, so the
// "content rejected, modify your input" signal is preserved.
func isContentSafetyErrorCode(code types.ErrorCode) bool {
	return code == types.ErrorCodePromptBlocked || code == types.ErrorCodeViolationFeeGrokCSAM
}

// withRequestId appends the request id to msg when one is present on the
// context, matching the format used elsewhere for support lookups.
func withRequestId(c *gin.Context, msg string) string {
	if c == nil {
		return msg
	}
	if requestId := c.GetString(common.RequestIdKey); requestId != "" {
		return common.MessageWithRequestId(msg, requestId)
	}
	return msg
}

// StandardUpstreamMessage returns the canned client-facing message for the
// given final HTTP status code, with the request id appended.
func StandardUpstreamMessage(c *gin.Context, statusCode int) string {
	msg, ok := standardUpstreamStatusMessages[statusCode]
	if !ok {
		msg = sanitizedUpstreamErrorMessage
	}
	return withRequestId(c, msg)
}

// ShouldSanitizeUpstreamForClient reports whether upstream error text must be
// hidden from the current caller: the feature is enabled and the caller is not
// an administrator. Administrators and root keep seeing the verbatim upstream
// error. A failed/absent user lookup resolves to non-admin, i.e. the safe
// (no-leak) direction.
func ShouldSanitizeUpstreamForClient(c *gin.Context) bool {
	if !setting.SanitizeUpstreamErrorEnabled {
		return false
	}
	if c != nil && model.IsAdmin(c.GetInt("id")) {
		return false
	}
	return true
}

// StandardizeUpstreamError replaces the client-facing message of an
// upstream-origin error with a fixed, sensitive-data-free message keyed by the
// final HTTP status code, when SANITIZE_UPSTREAM_ERROR is on and the caller is
// not an administrator. Err is left untouched, so error logs, retry
// classification and channel auto-disable keyword matching keep operating on
// the verbatim upstream error; administrators and root also receive the
// verbatim error in their own API response.
//
// Routing: locally-authored actionable errors keep their message; content-policy
// refusals get the provider-free content-safety message; everything else gets
// the status-code phrasing.
func StandardizeUpstreamError(c *gin.Context, e *types.NewAPIError) {
	if e == nil || !ShouldSanitizeUpstreamForClient(c) {
		return
	}
	code := e.GetErrorCode()
	// Provenance gates the allowlist: an error wrapped from an upstream
	// response (WithOpenAIError/WithClaudeError) carries a code the upstream
	// chose, so it must never match a local code and skip sanitization.
	if e.IsLocalError() && isLocalActionableErrorCode(code) {
		return
	}
	if isContentSafetyErrorCode(code) {
		e.SetClientMessage(withRequestId(c, contentSafetyClientMessage))
		return
	}
	e.SetClientMessage(StandardUpstreamMessage(c, e.StatusCode))
}

// StandardizeUpstreamTaskError applies the same standardization to a Task
// error's client-facing Message. Task/Midjourney paths carry their own
// TaskError/MidjourneyResponse types instead of NewAPIError, so they share the
// phrasing through this helper rather than through clientMessage. Only errors
// their author marked LocalError are left verbatim; the code is caller-chosen
// text and never grants that status by itself. Returns whether the message was
// replaced.
func StandardizeUpstreamTaskError(c *gin.Context, taskErr *taskdto.TaskError) bool {
	if taskErr == nil || taskErr.StatusCode < http.StatusBadRequest {
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if !ShouldSanitizeUpstreamForClient(c) {
		return false
	}
	if isContentSafetyErrorCode(types.ErrorCode(taskErr.Code)) {
		taskErr.Message = withRequestId(c, contentSafetyClientMessage)
		return true
	}
	taskErr.Message = StandardUpstreamMessage(c, taskErr.StatusCode)
	return true
}

func RelayErrorHandler(c *gin.Context, resp *http.Response, showBodyWhenFail bool) (newApiErr *types.NewAPIError) {
	newApiErr = types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, resp.StatusCode)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	CloseResponseBodyGracefully(resp)
	var errResponse dto.GeneralErrorResponse
	responseBodyText := string(responseBody)
	responseBodyPreview := common.LocalLogPreview(responseBodyText)
	buildErrWithBody := func(message string) error {
		if message == "" {
			return fmt.Errorf("bad response status code %d, body: %s", resp.StatusCode, responseBodyText)
		}
		return fmt.Errorf("bad response status code %d, message: %s, body: %s", resp.StatusCode, message, responseBodyText)
	}

	err = common.Unmarshal(responseBody, &errResponse)
	if err != nil {
		if showBodyWhenFail {
			newApiErr.Err = buildErrWithBody("")
		} else {
			logger.LogError(c, fmt.Sprintf("bad response status code %d, body: %s", resp.StatusCode, responseBodyPreview))
			newApiErr.Err = fmt.Errorf("bad response status code %d", resp.StatusCode)
		}
		return
	}

	if common.GetJsonType(errResponse.Error) == "object" {
		// General format error (OpenAI, Anthropic, Gemini, etc.)
		oaiError := errResponse.TryToOpenAIError()
		if oaiError != nil {
			newApiErr = types.WithOpenAIError(*oaiError, resp.StatusCode)
			if showBodyWhenFail {
				newApiErr.Err = buildErrWithBody(newApiErr.Error())
			}
			// Client-facing standardization is applied once at the relay exit
			// (controller.Relay defer), so the verbatim Err survives for logs,
			// retry classification and channel auto-disable here.
			return
		}
	}
	message := errResponse.ToMessage()
	if message == "" {
		// The body parsed as JSON but carried no usable error message; log the
		// raw body so the upstream failure remains diagnosable, and fall back to
		// a status-based message so client-facing projections do not degrade to
		// a bare error-type string when standardization is off.
		logger.LogError(c, fmt.Sprintf("bad response status code %d with empty error message, body: %s", resp.StatusCode, responseBodyPreview))
		message = fmt.Sprintf("bad response status code %d", resp.StatusCode)
	}
	newApiErr = types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
	if showBodyWhenFail {
		newApiErr.Err = buildErrWithBody(newApiErr.Error())
	}
	return
}

func ResetStatusCode(newApiErr *types.NewAPIError, statusCodeMappingStr string) {
	if newApiErr == nil {
		return
	}
	if statusCodeMappingStr == "" || statusCodeMappingStr == "{}" {
		return
	}
	statusCodeMapping := make(map[string]any)
	err := common.Unmarshal([]byte(statusCodeMappingStr), &statusCodeMapping)
	if err != nil {
		return
	}
	if newApiErr.StatusCode == http.StatusOK {
		return
	}
	codeStr := strconv.Itoa(newApiErr.StatusCode)
	if value, ok := statusCodeMapping[codeStr]; ok {
		intCode, ok := parseStatusCodeMappingValue(value)
		if !ok {
			return
		}
		newApiErr.StatusCode = intCode
	}
}

func parseStatusCodeMappingValue(value any) (int, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return 0, false
		}
		statusCode, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return statusCode, true
	case float64:
		if v != math.Trunc(v) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case json.Number:
		statusCode, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, false
		}
		return statusCode, true
	default:
		return 0, false
	}
}

func TaskErrorWrapperLocal(err error, code string, statusCode int) *taskdto.TaskError {
	openaiErr := TaskErrorWrapper(err, code, statusCode)
	openaiErr.LocalError = true
	return openaiErr
}

func TaskErrorWrapper(err error, code string, statusCode int) *taskdto.TaskError {
	text := err.Error()
	lowerText := strings.ToLower(text)
	if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
		common.SysLog(fmt.Sprintf("error: %s", text))
		//text = "请求上游地址失败"
		text = common.MaskSensitiveInfo(text)
	}
	//避免暴露内部错误
	taskError := &taskdto.TaskError{
		Code:       code,
		Message:    text,
		StatusCode: statusCode,
		Error:      err,
	}

	return taskError
}

// TaskErrorFromAPIError 将 PreConsumeBilling 返回的 NewAPIError 转换为 TaskError。
func TaskErrorFromAPIError(apiErr *types.NewAPIError) *taskdto.TaskError {
	if apiErr == nil {
		return nil
	}
	return &taskdto.TaskError{
		Code:       string(apiErr.GetErrorCode()),
		Message:    apiErr.Err.Error(),
		StatusCode: apiErr.StatusCode,
		LocalError: apiErr.IsLocalError(),
		Error:      apiErr.Err,
	}
}

// SanitizeUpstreamOpenAIError converts an upstream-provided OpenAIError payload
// into a client-safe one. The machine-readable type and code are preserved so
// clients can still branch on them, while the human-readable message is
// replaced by the standardized phrasing and upstream-only fields (Metadata,
// Param) are dropped. Used by transports that relay upstream error frames
// directly, such as the Realtime WebSocket proxy, which have no NewAPIError to
// standardize at the exit. Administrators and root keep seeing the verbatim
// error. statusCode is the status reported by the upstream frame, or 0 when the
// transport has no meaningful status.
func SanitizeUpstreamOpenAIError(c *gin.Context, upstream types.OpenAIError, statusCode int) types.OpenAIError {
	if !ShouldSanitizeUpstreamForClient(c) {
		return upstream
	}
	return types.OpenAIError{
		Message: StandardUpstreamMessage(c, statusCode),
		Type:    upstream.Type,
		Code:    upstream.Code,
	}
}
