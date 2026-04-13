package roblox

import (
	"net/http"
	"testing"
	"time"
)

func TestBuildHTTPRateLimitScopeKeyNormalizesNumericSegments(t *testing.T) {
	left := buildHTTPRateLimitScopeKey(http.MethodGet, "https://assetdelivery.roblox.com/v1/assetId/123456789")
	right := buildHTTPRateLimitScopeKey(http.MethodGet, "https://assetdelivery.roblox.com/v1/assetId/987654321")

	if left != right {
		t.Fatalf("expected numeric asset ids to normalize to the same scope, got %q and %q", left, right)
	}
}

func TestBuildHTTPRateLimitScopeKeySeparatesEndpoints(t *testing.T) {
	thumbnailScope := buildHTTPRateLimitScopeKey(http.MethodPost, "https://thumbnails.roblox.com/v1/batch")
	deliveryScope := buildHTTPRateLimitScopeKey(http.MethodGet, "https://assetdelivery.roblox.com/v1/assetId/123456789")

	if thumbnailScope == deliveryScope {
		t.Fatalf("expected different endpoints to keep separate scopes, got %q", thumbnailScope)
	}
}

func TestResolveHTTPRateLimitBackoffUsesRetryAfterSeconds(t *testing.T) {
	response := &http.Response{
		Header: http.Header{
			"Retry-After": []string{"7"},
		},
	}

	backoff := resolveHTTPRateLimitBackoff(response, 2*time.Second)
	if backoff != 7*time.Second {
		t.Fatalf("expected Retry-After seconds to win, got %v", backoff)
	}
}

func TestResolveHTTPRateLimitBackoffUsesRetryAfterHTTPDate(t *testing.T) {
	targetTime := time.Now().Add(3 * time.Second).UTC()
	response := &http.Response{
		Header: http.Header{
			"Retry-After": []string{targetTime.Format(http.TimeFormat)},
		},
	}

	backoff := resolveHTTPRateLimitBackoff(response, 2*time.Second)
	if backoff < 2*time.Second || backoff > 4*time.Second {
		t.Fatalf("expected HTTP-date Retry-After to produce about 3s of backoff, got %v", backoff)
	}
}

func TestResolveHTTPRateLimitBackoffFallsBackWhenHeaderMissing(t *testing.T) {
	backoff := resolveHTTPRateLimitBackoff(&http.Response{Header: http.Header{}}, 5*time.Second)
	if backoff != 5*time.Second {
		t.Fatalf("expected fallback backoff when Retry-After is missing, got %v", backoff)
	}
}
