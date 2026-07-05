package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"randomreviewer/internal/app"
	"randomreviewer/internal/config"
)

func main() {
	cfg, err := config.New()
	if err != nil {
		panic(fmt.Errorf("failed to load config: %w", err))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	botApp, err := app.New(ctx, cfg)
	if err != nil {
		panic(fmt.Errorf("failed to initialize bot: %w", err))
	}

	botApp.Start()
}
