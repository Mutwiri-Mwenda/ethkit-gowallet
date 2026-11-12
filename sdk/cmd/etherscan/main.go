package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Mutwiri-Mwenda/gowallet/sdk/explorer/etherscan"
)

func main() {
	apiKey := os.Getenv("ETHERSCAN_API_KEY")
	address := os.Getenv("ETH_WALLET_ADDRESS")

	if apiKey == "" {
		log.Fatal("ETHERSCAN_API_KEY is required")
	}

	if address == "" {
		log.Fatal("ETH_WALLET_ADDRESS is required")
	}

	client, err := etherscan.NewClient(apiKey)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	const sepolia uint64 = 11155111

	balance, err := client.GetBalance(
		ctx,
		sepolia,
		address,
	)
	if err != nil {
		log.Fatalf("get balance: %v", err)
	}

	fmt.Println("Network: Ethereum Sepolia")
	fmt.Printf("Chain ID: %d\n", sepolia)
	fmt.Printf("Wallet: %s\n", address)
	fmt.Printf("Balance (wei): %s\n", balance.String())
}
