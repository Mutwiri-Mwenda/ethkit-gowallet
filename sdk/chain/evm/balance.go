package evm

import (
	"context"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

var addressPattern = regexp.MustCompile(
	`^0x[0-9a-fA-F]{40}$`,
)

func (c *Client) GetBalance(
	ctx context.Context,
	address string,
) (*big.Int, error) {
	if !addressPattern.MatchString(address) {
		return nil, fmt.Errorf("invalid Ethereum address")
	}

	var hexBalance string

	err := c.rpc.Call(
		ctx,
		"eth_getBalance",
		[]any{address, "latest"},
		&hexBalance,
	)
	if err != nil {
		return nil, fmt.Errorf("fetch ETH balance: %w", err)
	}

	hexValue := strings.TrimPrefix(hexBalance, "0x")

	if !strings.HasPrefix(hexBalance, "0x") ||
		hexValue == "" {
		return nil, fmt.Errorf("invalid balance encoding")
	}

	balance, ok := new(big.Int).SetString(hexValue, 16)
	if !ok || balance.Sign() < 0 {
		return nil, fmt.Errorf("invalid hexadecimal balance")
	}

	return balance, nil
}
