package etherscan

import (
	"context"
	"errors"
	"math/big"
	"net/url"
	"regexp"
)

var (
	ErrInvalidAddress = errors.New("invalid Ethereum address")
	ErrInvalidBalance = errors.New("invalid Etherscan balance")
)

var addressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// GetBalance returns an account balance in wei.
func (c *Client) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	if !addressPattern.MatchString(address) {
		return nil, ErrInvalidAddress
	}

	params := url.Values{}
	params.Set("address", address)
	params.Set("tag", "latest")

	var encoded string

	err := c.call(
		ctx,
		chainID,
		"account",
		"balance",
		params,
		&encoded,
	)
	if err != nil {
		return nil, err
	}

	balance, ok := new(big.Int).SetString(encoded, 10)

	if !ok || balance.Sign() < 0 {
		return nil, ErrInvalidBalance
	}

	return balance, nil
}
