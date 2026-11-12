package gowallet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strings"
)

var addressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int    `json:"id"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	ID int `json:"id"`
}

func (c *Client) GetBalance(
	ctx context.Context,
	address string,
) (*big.Int, error) {
	if !addressPattern.MatchString(address) {
		return nil, errors.New("invalid Ethereum address")
	}

	payload, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		Method:  "eth_getBalance",
		Params:  []any{address, "latest"},
		ID:      1,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.rpcURL,
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RPC returned HTTP %d", resp.StatusCode)
	}

	var result rpcResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return nil, err
	}

	if result.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s",
			result.Error.Code, result.Error.Message)
	}
	if result.JSONRPC != "2.0" || result.ID != 1 {
		return nil, errors.New("invalid RPC response envelope")
	}

	var hexBalance string
	if err := json.Unmarshal(result.Result, &hexBalance); err != nil {
		return nil, fmt.Errorf("invalid balance result: %w", err)
	}

	hexBalance = strings.TrimPrefix(hexBalance, "0x")
	if hexBalance == "" {
		return nil, errors.New("empty balance result")
	}

	balance, ok := new(big.Int).SetString(hexBalance, 16)
	if !ok || balance.Sign() < 0 {
		return nil, errors.New("invalid hexadecimal balance")
	}

	return balance, nil
}
