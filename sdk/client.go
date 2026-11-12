package gowallet

import (
	"errors"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	rpcURL string
	http   *http.Client
}

func NewClient(rpcURL string) (*Client, error) {
	u, err := url.Parse(rpcURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid RPC URL")
	}

	return &Client{
		rpcURL: rpcURL,
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}
