package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const defaultBaseURL = "https://api.thundercompute.com:8443"

// Client is a thin, reusable HTTP client for the Thunder Compute API.
// It holds a single http.Client with TLS verification, connection pooling,
// and method-aware retry for safe GETs and the idempotent port PATCH.
type Client struct {
	baseURL    string
	apiToken   string
	userAgent  string
	httpClient *http.Client
}

type retryMethodContextKey struct{}

func NewClient(baseURL, apiToken, version string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = normalizeBaseURL(baseURL)
	if version == "" {
		version = "dev"
	}

	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 3
	retryClient.RetryWaitMin = 500 * time.Millisecond
	retryClient.RetryWaitMax = 5 * time.Second
	retryClient.Logger = nil // suppress default logger; we use tflog
	retryClient.HTTPClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	retryClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		method, _ := ctx.Value(retryMethodContextKey{}).(string)
		if method != http.MethodGet && method != http.MethodPatch {
			return false, nil
		}
		return retryablehttp.ErrorPropagatedRetryPolicy(ctx, resp, err)
	}
	// Preserve the final HTTP response so doRequest can decode its structured
	// API error after safe-method retries are exhausted.
	retryClient.ErrorHandler = retryablehttp.PassthroughErrorHandler

	return &Client{
		baseURL:    baseURL,
		apiToken:   apiToken,
		userAgent:  "terraform-provider-thundercompute/" + version,
		httpClient: retryClient.StandardClient(),
	}
}

func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	for _, suffix := range []string{"/v1", "/v2"} {
		if strings.HasSuffix(baseURL, suffix) {
			return strings.TrimSuffix(baseURL, suffix)
		}
	}
	return baseURL
}

// doRequest executes an authenticated API request.
// body is JSON-marshaled if non-nil. result is JSON-unmarshaled from the response if non-nil.
func (c *Client) doRequest(ctx context.Context, method, path string, body, result interface{}) error {
	fullURL, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return fmt.Errorf("building url for %s: %w", path, err)
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	tflog.Debug(ctx, "thunder api request", map[string]interface{}{
		"method": method,
		"path":   path,
	})

	requestCtx := context.WithValue(ctx, retryMethodContextKey{}, method)
	req, err := http.NewRequestWithContext(requestCtx, method, fullURL, bodyReader)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	// Cap response body at 10MB to prevent memory exhaustion from malformed responses
	const maxResponseSize = 10 << 20
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		_ = json.Unmarshal(respBody, apiErr)
		if apiErr.ErrorType == "" {
			apiErr.ErrorType = http.StatusText(resp.StatusCode)
			if apiErr.Message == "" && len(respBody) > 0 {
				n := len(respBody)
				if n > 200 {
					n = 200
				}
				apiErr.Message = string(respBody[:n])
			}
		}
		tflog.Warn(ctx, "thunder api error", map[string]interface{}{
			"method":      method,
			"path":        path,
			"status_code": resp.StatusCode,
			"error_type":  apiErr.ErrorType,
		})
		return apiErr
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshaling response: %w", err)
		}
	}
	return nil
}
