package evm

import "github.com/Mutwiri-Mwenda/gowallet/sdk/rpc"

type Client struct {
	rpc *rpc.Client
}

func NewClient(rpcClient *rpc.Client) *Client {
	return &Client{
		rpc: rpcClient,
	}
}
