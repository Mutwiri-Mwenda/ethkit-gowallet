package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

var (
	ErrInvalidChainID = errors.New("invalid chain ID")
	ErrChainMismatch  = errors.New("unexpected Ethereum chain")
)

const (
	EthereumMainnet uint64 = 1
	Sepolia         uint64 = 11155111
)

type ChainMismatchError struct {
	Expected uint64
	Actual   uint64
}

func (e *ChainMismatchError) Error() string {
	return fmt.Sprintf(
		"Ethereum chain mismatch: expected %d, got %d",
		e.Expected,
		e.Actual,
	)
}

func (e *ChainMismatchError) Is(target error) bool {
	return target == ErrChainMismatch
}

// ChainID retrieves the network identifier from the RPC provider.
func (c *Client) ChainID(ctx context.Context) (uint64, error) {
	if c == nil || c.rpc == nil {
		return 0, ErrInvalidChainID
	}

	var encoded string

	if err := c.rpc.Call(
		ctx,
		"eth_chainId",
		[]any{},
		&encoded,
	); err != nil {
		return 0, fmt.Errorf("get Ethereum chain ID: %w", err)
	}

	if !strings.HasPrefix(encoded, "0x") {
		return 0, ErrInvalidChainID
	}

	hexValue := encoded[2:]

	if hexValue == "" {
		return 0, ErrInvalidChainID
	}

	id, ok := new(big.Int).SetString(hexValue, 16)
	if !ok || id.Sign() <= 0 || !id.IsUint64() {
		return 0, ErrInvalidChainID
	}

	return id.Uint64(), nil
}

// VerifyChainID checks that the provider uses the expected network.
func (c *Client) VerifyChainID(
	ctx context.Context,
	expected uint64,
) error {
	if expected == 0 {
		return ErrInvalidChainID
	}

	actual, err := c.ChainID(ctx)
	if err != nil {
		return err
	}

	if actual != expected {
		return &ChainMismatchError{
			Expected: expected,
			Actual:   actual,
		}
	}

	return nil
}
