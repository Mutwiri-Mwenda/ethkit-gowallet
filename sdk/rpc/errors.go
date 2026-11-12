package rpc

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidEndpoint = errors.New("invalid RPC endpoint")
	ErrInvalidResponse = errors.New("invalid RPC response")
	ErrTransport       = errors.New("RPC transport failure")
	ErrHTTPStatus      = errors.New("unexpected RPC HTTP status")
	ErrInvalidRequest  = errors.New("invalid RPC request")
)

type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("RPC provider returned HTTP %d", e.StatusCode)
}

func (e *HTTPError) Is(target error) bool {
	return target == ErrHTTPStatus
}

type ProtocolError struct {
	Reason string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("invalid RPC response: %s", e.Reason)
}

func (e *ProtocolError) Is(target error) bool {
	return target == ErrInvalidResponse
}
