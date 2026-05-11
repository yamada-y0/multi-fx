package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/yamada/fxd/pkg/currency"
	pkgorder "github.com/yamada/fxd/pkg/order"
)

func runSnapshot(args []string) {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	stateDir := fs.String("state-dir", "", "エージェントディレクトリパス（必須）")
	pair := fs.String("pair", "USDJPY", "通貨ペア")
	fs.Parse(args)

	if *stateDir == "" {
		fmt.Fprintln(os.Stderr, "Usage: fxd snapshot --state-dir <dir> [--pair <pair>]")
		os.Exit(1)
	}

	ctx := context.Background()
	tb, _, _, err := setupBrokers(*stateDir, "", currency.Pair(*pair))
	if err != nil {
		log.Fatalf("setup broker: %v", err)
	}

	account, err := tb.FetchAccount(ctx)
	if err != nil {
		log.Fatalf("fetch account: %v", err)
	}
	positions, err := tb.FetchPositions(ctx)
	if err != nil {
		log.Fatalf("fetch positions: %v", err)
	}
	orders, err := tb.FetchOrders(ctx)
	if err != nil {
		log.Fatalf("fetch orders: %v", err)
	}

	type snapshotOutput struct {
		Account   pkgorder.AccountInfo    `json:"Account"`
		Positions []pkgorder.Position     `json:"Positions"`
		Orders    []pkgorder.PendingOrder `json:"Orders"`
	}
	printJSON(snapshotOutput{Account: account, Positions: positions, Orders: orders})
}
