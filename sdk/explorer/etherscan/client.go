package etherscan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	baseURL          = "https://api.etherscan.io/v2/api"
	maxResponseBytes = 1 << 20
)

var (
	ErrInvalidAPIKey   = errors.New("invalid Etherscan API key")
	ErrInvalidRequest  = errors.New("invalid Etherscan request")
	ErrAPI             = errors.New("Etherscan API error")
	ErrInvalidResponse = errors.New("invalid Etherscan response")
	ErrTransport       = errors.New("Etherscan transport failure")
	ErrRateLimited     = errors.New("Etherscan rate limit exceeded")
)

type Client struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

type APIError struct {
	StatusCode int
	Message    string
	RateLimit  bool
}

func (e *APIError) Error() string {
	if e.RateLimit {
		return "Etherscan rate limit exceeded"
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf(
			"Etherscan API returned HTTP %d",
			e.StatusCode,
		)
	}
	return "Etherscan API rejected the request"
}

func (e *APIError) Is(target error) bool {
	return target == ErrAPI ||
		(target == ErrRateLimited && e.RateLimit)
}

type response struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

func NewClient(apiKey string) (*Client, error) {
	return newClient(apiKey, baseURL)
}

// newClient allows package tests to inject a local server.
// Production callers use NewClient and the fixed HTTPS URL.
func newClient(apiKey, endpoint string) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" ||
		strings.TrimSpace(apiKey) != apiKey ||
		strings.ContainsAny(apiKey, " \t\r\n") {
		return nil, ErrInvalidAPIKey
	}

	u, err := url.Parse(endpoint)
	if err != nil || u == nil ||
		u.Hostname() == "" ||
		u.User != nil ||
		u.Fragment != "" ||
		u.RawQuery != "" {
		return nil, ErrInvalidRequest
	}

	if u.Scheme != "https" {
		host := u.Hostname()
		if u.Scheme != "http" ||
			(host != "localhost" && host != "127.0.0.1" &&
				host != "::1") {
			return nil, ErrInvalidRequest
		}
	}

	return &Client{
		apiKey:   apiKey,
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(
				req *http.Request,
				via []*http.Request,
			) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (c *Client) call(
	ctx context.Context,
	chainID uint64,
	module string,
	action string,
	params url.Values,
	result any,
) error {
	if c == nil || c.httpClient == nil ||
		ctx == nil || chainID == 0 ||
		module == "" || action == "" || result == nil {
		return ErrInvalidRequest
	}

	u, err := url.Parse(c.endpoint)
	if err != nil {
		return ErrInvalidRequest
	}

	query := url.Values{}
	query.Set("chainid", fmt.Sprint(chainID))
	query.Set("module", module)
	query.Set("action", action)

	for key, values := range params {
		switch key {
		case "apikey", "chainid", "module", "action":
			return ErrInvalidRequest
		}
		for _, value := range values {
			query.Add(key, value)
		}
	}

	query.Set("apikey", c.apiKey)
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		u.String(),
		nil,
	)
	if err != nil {
		return ErrInvalidRequest
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Never return the underlying HTTP error directly:
		// it may contain the URL and its API key.
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", ErrTransport, ctx.Err())
		}

		var netErr interface {
			Timeout() bool
		}
		if errors.As(err, &netErr) && netErr.Timeout() {
			return fmt.Errorf(
				"%w: %w",
				ErrTransport,
				context.DeadlineExceeded,
			)
		}
		return ErrTransport
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return &APIError{
			StatusCode: resp.StatusCode,
			RateLimit:  true,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return &APIError{StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(
		io.LimitReader(resp.Body, maxResponseBytes+1),
	)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", ErrTransport, ctx.Err())
		}
		return ErrTransport
	}

	if len(body) > maxResponseBytes {
		return ErrInvalidResponse
	}

	var envelope response
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ErrInvalidResponse
	}

	if envelope.Status != "1" {
		rateLimited := false

		var message string
		_ = json.Unmarshal(envelope.Result, &message)

		lower := strings.ToLower(envelope.Message + " " + message)
		if strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "max rate") {
			rateLimited = true
		}

		return &APIError{RateLimit: rateLimited}
	}

	if len(envelope.Result) == 0 ||
		bytes.Equal(bytes.TrimSpace(envelope.Result), []byte("null")) {
		return ErrInvalidResponse
	}

	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return ErrInvalidResponse
	}

	return nil
}
