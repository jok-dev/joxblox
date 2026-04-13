package roblox

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"joxblox/internal/debug"
)

const (
	robloxThumbnailURLTemplate  = "https://thumbnails.roblox.com/v1/assets?assetIds=%d&size=768x432&format=Png&isCircular=false"
	robloxThumbnailBatchURL     = "https://thumbnails.roblox.com/v1/batch"
	robloxAssetDeliveryURLBase  = "https://assetdelivery.roblox.com/v1/assetId/%d"
	robloxEconomyDetailsURLBase = "https://economy.roblox.com/v2/assets/%d/details"
	robloxAuthenticatedUserURL  = "https://users.roblox.com/v1/users/authenticated"
)

type HttpRateLimitPolicy struct {
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	MaxRetries     int
}

var defaultHTTPRateLimitPolicy = HttpRateLimitPolicy{
	InitialBackoff: 2 * time.Second,
	MaxBackoff:     30 * time.Second,
	MaxRetries:     0,
}

var sharedHTTPRateLimitState = struct {
	mutex               sync.Mutex
	blockedUntilByScope map[string]time.Time
}{
	blockedUntilByScope: map[string]time.Time{},
}

func DoAuthenticatedGet(urlString string, timeout time.Duration) (*http.Response, error) {
	return DoRequest(http.MethodGet, urlString, nil, GetRoblosecurityCookieHeader(), timeout, nil)
}

func DoGetWithCookie(urlString string, cookieHeader string, timeout time.Duration) (*http.Response, error) {
	return DoRequest(http.MethodGet, urlString, nil, cookieHeader, timeout, nil)
}

func DoRequest(method string, urlString string, body io.Reader, cookieHeader string, timeout time.Duration, headers map[string]string) (*http.Response, error) {
	return DoRequestWithRateLimitPolicy(method, urlString, body, cookieHeader, timeout, headers, defaultHTTPRateLimitPolicy)
}

func DoRequestWithRateLimitPolicy(method string, urlString string, body io.Reader, cookieHeader string, timeout time.Duration, headers map[string]string, policy HttpRateLimitPolicy) (*http.Response, error) {
	var bodyBytes []byte
	if body != nil {
		var readErr error
		bodyBytes, readErr = io.ReadAll(body)
		if readErr != nil {
			return nil, readErr
		}
	}

	backoff := policy.InitialBackoff
	if backoff <= 0 {
		backoff = defaultHTTPRateLimitPolicy.InitialBackoff
	}
	maxBackoff := policy.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = defaultHTTPRateLimitPolicy.MaxBackoff
	}
	maxRetries := policy.MaxRetries
	unlimitedRetries := maxRetries <= 0
	rateLimitScopeKey := buildHTTPRateLimitScopeKey(method, urlString)
	for attempt := 0; ; attempt++ {
		waitForSharedHTTPRateLimitScope(rateLimitScopeKey)

		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		httpClient := &http.Client{Timeout: timeout}
		request, requestErr := http.NewRequest(method, urlString, bodyReader)
		if requestErr != nil {
			return nil, requestErr
		}
		if strings.TrimSpace(cookieHeader) != "" {
			request.Header.Set("Cookie", cookieHeader)
		}
		for headerName, headerValue := range headers {
			request.Header.Set(headerName, headerValue)
		}

		response, doErr := httpClient.Do(request)
		if doErr != nil {
			return nil, doErr
		}
		if response.StatusCode != http.StatusTooManyRequests {
			return response, nil
		}
		if !unlimitedRetries && attempt >= maxRetries {
			return response, nil
		}

		backoffDuration := resolveHTTPRateLimitBackoff(response, backoff)
		response.Body.Close()
		blockSharedHTTPRateLimitScope(rateLimitScopeKey, backoffDuration)
		if unlimitedRetries {
			debug.Logf(
				"HTTP 429 on %s %s, blocking scope %s for %v (attempt %d)",
				method,
				urlString,
				rateLimitScopeKey,
				backoffDuration,
				attempt+1,
			)
		} else {
			debug.Logf(
				"HTTP 429 on %s %s, blocking scope %s for %v (attempt %d/%d)",
				method,
				urlString,
				rateLimitScopeKey,
				backoffDuration,
				attempt+1,
				maxRetries,
			)
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func buildHTTPRateLimitScopeKey(method string, urlString string) string {
	parsedURL, err := url.Parse(strings.TrimSpace(urlString))
	if err != nil || parsedURL == nil {
		return strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(urlString)
	}

	pathSegments := strings.Split(parsedURL.EscapedPath(), "/")
	for index, segment := range pathSegments {
		if shouldNormalizeHTTPRateLimitSegment(segment) {
			pathSegments[index] = ":id"
		}
	}

	normalizedPath := strings.Join(pathSegments, "/")
	if normalizedPath == "" {
		normalizedPath = "/"
	}
	return fmt.Sprintf(
		"%s %s://%s%s",
		strings.ToUpper(strings.TrimSpace(method)),
		strings.ToLower(strings.TrimSpace(parsedURL.Scheme)),
		strings.ToLower(strings.TrimSpace(parsedURL.Host)),
		normalizedPath,
	)
}

func shouldNormalizeHTTPRateLimitSegment(segment string) bool {
	trimmedSegment := strings.TrimSpace(segment)
	if trimmedSegment == "" {
		return false
	}
	for _, char := range trimmedSegment {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func waitForSharedHTTPRateLimitScope(scopeKey string) {
	for {
		sharedHTTPRateLimitState.mutex.Lock()
		blockedUntil := sharedHTTPRateLimitState.blockedUntilByScope[scopeKey]
		waitDuration := time.Until(blockedUntil)
		sharedHTTPRateLimitState.mutex.Unlock()

		if waitDuration <= 0 {
			return
		}
		time.Sleep(waitDuration)
	}
}

func blockSharedHTTPRateLimitScope(scopeKey string, duration time.Duration) {
	if duration <= 0 || strings.TrimSpace(scopeKey) == "" {
		return
	}

	blockedUntil := time.Now().Add(duration)
	sharedHTTPRateLimitState.mutex.Lock()
	currentBlockedUntil := sharedHTTPRateLimitState.blockedUntilByScope[scopeKey]
	if blockedUntil.After(currentBlockedUntil) {
		sharedHTTPRateLimitState.blockedUntilByScope[scopeKey] = blockedUntil
	}
	sharedHTTPRateLimitState.mutex.Unlock()
}

func resolveHTTPRateLimitBackoff(response *http.Response, fallback time.Duration) time.Duration {
	if response != nil {
		retryAfterHeader := strings.TrimSpace(response.Header.Get("Retry-After"))
		if retryAfterHeader != "" {
			if retryAfterSeconds, err := time.ParseDuration(retryAfterHeader + "s"); err == nil && retryAfterSeconds > 0 {
				return retryAfterSeconds
			}
			if retryAfterTime, err := http.ParseTime(retryAfterHeader); err == nil {
				retryAfterDuration := time.Until(retryAfterTime)
				if retryAfterDuration > 0 {
					return retryAfterDuration
				}
			}
		}
	}
	if fallback > 0 {
		return fallback
	}
	return defaultHTTPRateLimitPolicy.InitialBackoff
}

func DoThumbnailGet(assetID int64, timeout time.Duration) (*http.Response, error) {
	return DoAuthenticatedGet(fmt.Sprintf(robloxThumbnailURLTemplate, assetID), timeout)
}

func DoThumbnailBatchPost(body io.Reader, timeout time.Duration) (*http.Response, error) {
	return DoRequest(
		http.MethodPost,
		robloxThumbnailBatchURL,
		body,
		"",
		timeout,
		map[string]string{
			"Content-Type": "application/json",
		},
	)
}

func DoAssetDeliveryGet(assetID int64, timeout time.Duration) (*http.Response, error) {
	return DoAuthenticatedGet(fmt.Sprintf(robloxAssetDeliveryURLBase, assetID), timeout)
}

func DoEconomyDetailsGet(assetID int64, timeout time.Duration) (*http.Response, error) {
	return DoAuthenticatedGet(fmt.Sprintf(robloxEconomyDetailsURLBase, assetID), timeout)
}

func DoAuthenticatedUserGet(cookieHeader string, timeout time.Duration) (*http.Response, error) {
	return DoGetWithCookie(robloxAuthenticatedUserURL, cookieHeader, timeout)
}
