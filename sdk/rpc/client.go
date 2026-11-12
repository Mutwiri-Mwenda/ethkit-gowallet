package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const maxResponseBytes = 1 << 20

type Client struct {
	endpoint   string
	httpClient *http.Client
	nextID     atomic.Uint64
}

func NewClient(endpoint string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u == nil {
		return nil, ErrInvalidEndpoint
	}

	if u.Hostname() == "" ||
		u.User != nil ||
		u.Fragment != "" ||
		u.Opaque != "" {
		return nil, ErrInvalidEndpoint
	}

	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		ip := net.ParseIP(host)

		if !strings.EqualFold(host, "localhost") &&
			(ip == nil || !ip.IsLoopback()) {
			return nil, ErrInvalidEndpoint
		}
	default:
		return nil, ErrInvalidEndpoint
	}

	return &Client{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (c *Client) Call(
	ctx context.Context,
	method string,
	params any,
	result any,
) error {
	if c == nil || c.httpClient == nil {
		return ErrInvalidRequest
	}

	if ctx == nil || strings.TrimSpace(method) == "" ||
		result == nil {
		return ErrInvalidRequest
	}

	id := c.nextID.Add(1)

	payload, err := json.Marshal(Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	})
	if err != nil {
		return fmt.Errorf("%w: encode request", ErrInvalidRequest)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("%w: create request", ErrInvalidRequest)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: %w", ErrTransport, ctx.Err())
		}
		return fmt.Errorf("%w: %w", ErrTransport, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &HTTPError{StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(
		io.LimitReader(resp.Body, maxResponseBytes+1),
	)
	if err != nil {
		return fmt.Errorf("%w: read response", ErrTransport)
	}

	if len(body) > maxResponseBytes {
		return &ProtocolError{Reason: "response too large"}
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil ||
		fields == nil {
		return &ProtocolError{Reason: "malformed JSON object"}
	}

	var version string
	if err := json.Unmarshal(
		fields["jsonrpc"], &version,
	); err != nil || version != "2.0" {
		return &ProtocolError{Reason: "invalid protocol version"}
	}

	var responseID uint64
	if err := json.Unmarshal(
		fields["id"], &responseID,
	); err != nil || responseID != id {
		return &ProtocolError{Reason: "response ID mismatch"}
	}

	rawResult, hasResult := fields["result"]
	rawError, hasError := fields["error"]

	if hasResult == hasError {
		return &ProtocolError{
			Reason: "expected exactly one of result or error",
		}
	}

	if hasError {
		if bytes.Equal(rawError, []byte("null")) {
			return &ProtocolError{Reason: "null error object"}
		}

		var rpcErr struct {
			Code    *int    `json:"code"`
			Message *string `json:"message"`
		}

		if err := json.Unmarshal(rawError, &rpcErr); err != nil ||
			rpcErr.Code == nil || rpcErr.Message == nil {
			return &ProtocolError{Reason: "invalid error object"}
		}

		return &RPCError{
			Code:    *rpcErr.Code,
			Message: *rpcErr.Message,
		}
	}

	if len(rawResult) == 0 ||
		bytes.Equal(rawResult, []byte("null")) {
		return &ProtocolError{Reason: "missing or null result"}
	}

	if err := json.Unmarshal(rawResult, result); err != nil {
		return &ProtocolError{Reason: "invalid result encoding"}
	}

	return nil
}

func IsTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) == false &&
			isNetworkTimeout(err)
}

func isNetworkTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
