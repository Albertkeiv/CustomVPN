package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"customvpn/client/internal/app"
	"customvpn/client/internal/config"
	"customvpn/client/internal/logging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	appDir, err := config.DetectAppDir()
	if err != nil {
		return fmt.Errorf("determine app directory: %w", err)
	}
	defaultConfig := config.DefaultPath(appDir)
	configPath := flag.String("config", defaultConfig, "path to config.yaml")
	flag.Parse()

	cfg, err := config.Load(*configPath, appDir)
	if err != nil {
		return err
	}

	logLevel := logging.ParseLevel(cfg.LogLevel)
	logger, err := logging.New(cfg.LogFile, logLevel)
	if err != nil {
		return fmt.Errorf("initialize logger: %w", err)
	}
	defer logger.Close()

	baseCtx := logging.WithContext(context.Background(), logger)
	ctx, stop := signal.NotifyContext(baseCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Infof("CustomVPN client starting (config: %s)", *configPath)
	logger.Debugf("core binary: %s", cfg.CorePath)
	logger.Debugf("core log file: %s", cfg.CoreLogFile)

	return startApp(ctx, cfg)
}

func startApp(ctx context.Context, cfg *config.Config) error {
	logger, ok := logging.FromContext(ctx)
	if !ok {
		return fmt.Errorf("logger not found in context")
	}
	application, err := app.New(cfg, logger)
	if err != nil {
		return err
	}
	if err := application.Run(); err != nil {
		return err
	}
	logger.Infof("state machine launched, entering UI loop")
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			logger.Infof("shutdown requested")
			application.Stop()
		case <-application.Done():
			logger.Infof("application requested shutdown")
		}
		close(done)
	}()
	application.RunUILoop()
	logger.Infof("UI loop exited, stopping application")
	application.Stop()
	<-done
	return nil
}
