package app

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	robloxThumbnailURLTemplate  = "https://thumbnails.roblox.com/v1/assets?assetIds=%d&size=768x432&format=Png&isCircular=false"
	robloxAssetDeliveryURLBase  = "https://assetdelivery.roblox.com/v1/assetId/%d"
	robloxEconomyDetailsURLBase = "https://economy.roblox.com/v2/assets/%d/details"
	robloxAuthenticatedUserURL  = "https://users.roblox.com/v1/users/authenticated"
)

func doRobloxAuthenticatedGet(urlString string, timeout time.Duration) (*http.Response, error) {
	return doRobloxRequest(http.MethodGet, urlString, nil, GetRoblosecurityCookieHeader(), timeout, nil)
}

func doRobloxGetWithCookie(urlString string, cookieHeader string, timeout time.Duration) (*http.Response, error) {
	return doRobloxRequest(http.MethodGet, urlString, nil, cookieHeader, timeout, nil)
}

func doRobloxRequest(method string, urlString string, body io.Reader, cookieHeader string, timeout time.Duration, headers map[string]string) (*http.Response, error) {
	httpClient := &http.Client{Timeout: timeout}
	request, requestErr := http.NewRequest(method, urlString, body)
	if requestErr != nil {
		return nil, requestErr
	}

	if strings.TrimSpace(cookieHeader) != "" {
		request.Header.Set("Cookie", cookieHeader)
	}
	for headerName, headerValue := range headers {
		request.Header.Set(headerName, headerValue)
	}
	return httpClient.Do(request)
}

func doRobloxThumbnailGet(assetID int64, timeout time.Duration) (*http.Response, error) {
	return doRobloxAuthenticatedGet(fmt.Sprintf(robloxThumbnailURLTemplate, assetID), timeout)
}

func doRobloxAssetDeliveryGet(assetID int64, timeout time.Duration) (*http.Response, error) {
	return doRobloxAuthenticatedGet(fmt.Sprintf(robloxAssetDeliveryURLBase, assetID), timeout)
}

func doRobloxEconomyDetailsGet(assetID int64, timeout time.Duration) (*http.Response, error) {
	return doRobloxAuthenticatedGet(fmt.Sprintf(robloxEconomyDetailsURLBase, assetID), timeout)
}

func doRobloxAuthenticatedUserGet(cookieHeader string, timeout time.Duration) (*http.Response, error) {
	return doRobloxGetWithCookie(robloxAuthenticatedUserURL, cookieHeader, timeout)
}
