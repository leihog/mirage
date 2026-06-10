package web

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mirage/internal/mailbox"
)

type unsubscribeInfo struct {
	URL string
}

type unsubscribeResult struct {
	URL              string            `json:"url"`
	Success          bool              `json:"success"`
	StatusCode       int               `json:"statusCode"`
	Status           string            `json:"status"`
	RequestMethod    string            `json:"requestMethod"`
	RequestHeaders   map[string]string `json:"requestHeaders"`
	RequestBody      string            `json:"requestBody"`
	ResponseHeaders  map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody     string            `json:"responseBody,omitempty"`
	ResponseBodySize int               `json:"responseBodySize"`
	DurationMS       int64             `json:"durationMs"`
	Error            string            `json:"error,omitempty"`
}

func unsubscribeAction(msg mailbox.Message) *unsubscribeInfo {
	post := strings.TrimSpace(headerValue(msg.Headers, "List-Unsubscribe-Post"))
	if !strings.EqualFold(post, "List-Unsubscribe=One-Click") {
		return nil
	}

	for _, candidate := range unsubscribeCandidates(headerValue(msg.Headers, "List-Unsubscribe")) {
		parsed, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return &unsubscribeInfo{URL: parsed.String()}
		}
	}
	return nil
}

func sendOneClickUnsubscribe(target string) unsubscribeResult {
	requestBody := "List-Unsubscribe=One-Click"
	result := unsubscribeResult{
		URL:           target,
		RequestMethod: http.MethodPost,
		RequestHeaders: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		RequestBody: requestBody,
	}
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(requestBody))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	res, err := client.Do(req)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer res.Body.Close()

	result.StatusCode = res.StatusCode
	result.Status = http.StatusText(res.StatusCode)
	result.Success = res.StatusCode >= http.StatusOK && res.StatusCode < http.StatusMultipleChoices
	result.ResponseHeaders = flattenHeaders(res.Header)

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.ResponseBody = string(body)
	result.ResponseBodySize = len(body)
	return result
}

func flattenHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		out[key] = strings.Join(values, ", ")
	}
	return out
}

func headerValue(headers map[string]string, key string) string {
	for headerKey, value := range headers {
		if strings.EqualFold(headerKey, key) {
			return value
		}
	}
	return ""
}

func unsubscribeCandidates(value string) []string {
	var candidates []string
	for i := 0; i < len(value); i++ {
		if value[i] != '<' {
			continue
		}
		end := strings.IndexByte(value[i+1:], '>')
		if end == -1 {
			break
		}
		candidate := strings.TrimSpace(value[i+1 : i+1+end])
		if candidate != "" {
			candidates = append(candidates, candidate)
		}
		i += end + 1
	}
	if len(candidates) > 0 {
		return candidates
	}

	for _, candidate := range strings.Split(value, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}
