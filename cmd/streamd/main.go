package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/streamd/streamd/internal/config"
	"github.com/streamd/streamd/internal/daemon"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		if err := runStart(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := runStatus(); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "stop":
		if err := runStop(); err != nil {
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
	fmt.Println(`streamd - peer-to-peer audio streaming sender daemon

Usage:
  streamd start [--foreground]
  streamd status
  streamd stop

Configuration:
  ~/.streamd/config.yaml`)
}

func runStart(args []string) error {
	foreground := false
	for _, arg := range args {
		if arg == "--foreground" || arg == "-f" {
			foreground = true
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := config.EnsureConfigDir(); err != nil {
		return err
	}

	pidPath, err := config.PIDPath()
	if err != nil {
		return err
	}

	running, pid, err := daemon.IsRunning(pidPath)
	if err != nil {
		return err
	}
	if running {
		return fmt.Errorf("streamd is already running (pid %d)", pid)
	}

	logPath, err := config.LogPath()
	if err != nil {
		return err
	}

	if !foreground {
		if err := daemonize(logPath); err != nil {
			return err
		}
	}

	if err := daemon.WritePID(pidPath); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	logger, err := daemon.SetupLogger(cfg.LogLevel, logPath)
	if err != nil {
		return err
	}

	logger.Info("Stream daemon started")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("Shutdown signal received")
		cancel()
	}()

	defer func() {
		_ = daemon.RemovePID(pidPath)
	}()

	d := daemon.New(cfg, logger)
	if err := d.Run(ctx); err != nil && err != context.Canceled {
		return err
	}

	logger.Info("Stream daemon stopped")
	return nil
}

func daemonize(logPath string) error {
	if os.Getenv("STREAMD_FOREGROUND") == "1" {
		return nil
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	attr := &syscall.ProcAttr{
		Env:   append(os.Environ(), "STREAMD_FOREGROUND=1"),
		Files: []uintptr{os.Stdin.Fd(), logFile.Fd(), logFile.Fd()},
		Sys:   &syscall.SysProcAttr{Setsid: true},
	}

	pid, err := syscall.ForkExec(os.Args[0], append([]string{os.Args[0], "start", "--foreground"}, os.Args[2:]...), attr)
	if err != nil {
		_ = logFile.Close()
		return fmt.Errorf("fork daemon: %w", err)
	}

	fmt.Printf("streamd started (pid %d)\n", pid)
	os.Exit(0)
	return nil
}

func runStatus() error {
	pidPath, err := config.PIDPath()
	if err != nil {
		return err
	}

	running, pid, err := daemon.IsRunning(pidPath)
	if err != nil {
		return err
	}

	if running {
		fmt.Printf("streamd is running (pid %d)\n", pid)
		return nil
	}

	fmt.Println("streamd is not running")
	return nil
}

func runStop() error {
	pidPath, err := config.PIDPath()
	if err != nil {
		return err
	}

	if err := daemon.Stop(pidPath); err != nil {
		return err
	}

	fmt.Println("streamd stopped")
	return nil
}
