package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/streamd/streamd/internal/config"
	"github.com/streamd/streamd/internal/daemon"
	"github.com/streamd/streamd/internal/receiver"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "connect":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: streamctl connect <peer-id>")
			os.Exit(1)
		}
		if err := runConnect(os.Args[2]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "list":
		if err := runList(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`streamctl - peer-to-peer audio streaming receiver

Usage:
  streamctl connect <peer-id>
  streamctl list

Configuration:
  ~/.streamd/config.yaml`)
}

func runConnect(senderID string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := daemon.SetupLogger(cfg.LogLevel, "")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	client := receiver.NewClient(cfg, logger)
	return client.Connect(ctx, senderID)
}

func runList() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := daemon.SetupLogger(cfg.LogLevel, "")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := receiver.NewClient(cfg, logger)
	peers, err := client.ListPeers(ctx)
	if err != nil {
		return err
	}

	if len(peers) == 0 {
		fmt.Println("No online peers found")
		return nil
	}

	ids := make([]string, 0, len(peers))
	for id := range peers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	fmt.Println("Online peers:")
	for _, id := range ids {
		ts := peers[id]
		fmt.Printf("  %s  (last seen %s)\n", id, ts.Format(time.RFC3339))
	}
	return nil
}
