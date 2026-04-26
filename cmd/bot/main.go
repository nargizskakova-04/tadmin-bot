package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"admin-bot/internal/config"
	"admin-bot/internal/infra/oneedu"
	"admin-bot/internal/infra/scheduler"
	"admin-bot/internal/infra/sheets"
	"admin-bot/internal/infra/telegram"
	"admin-bot/internal/infra/templates"
	"admin-bot/internal/usecase"
	"admin-bot/internal/usecase/strategy"

	delivery "admin-bot/internal/delivery/telegram"
)

func main() {
	_ = godotenv.Load()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	// Infrastructure
	eduClient := oneedu.NewClient(cfg.OneEduBaseURL, cfg.OneEduAccessToken, logger)
	tmplLoader := templates.NewFileLoader(cfg.TemplatesPath)

	// Google Sheets (optional)
	var sheetsClient *sheets.Client
	if cfg.GoogleCredentialsFile != "" {
		sheetsClient, err = sheets.NewClient(cfg.GoogleCredentialsFile, logger)
		if err != nil {
			logger.Error("failed to create sheets client (continuing without it)", "err", err)
		} else {
			logger.Info("google sheets client initialized")
		}
	} else {
		logger.Warn("GOOGLE_CREDENTIALS_FILE not set — sheet creation will be unavailable")
	}

	// Strategies
	strategies := []strategy.PiscineStrategy{
		strategy.NewGoStrategy(),
		strategy.NewJSStrategy(),
		strategy.NewAIStrategy(),
	}

	// Use case
	raidUC := usecase.NewRaidUseCase(eduClient, tmplLoader, strategies)

	// Telegram
	tgAdapter, err := telegram.NewAdapter(cfg.TelegramToken, logger)
	if err != nil {
		logger.Error("failed to create telegram adapter", "err", err)
		os.Exit(1)
	}

	handler := delivery.NewHandler(raidUC, tgAdapter, sheetsClient, logger)
	delivery.RegisterHandlers(tgAdapter.Bot(), handler)

	// Scheduler
	sched := scheduler.NewCronScheduler(raidUC, tgAdapter, cfg.ChatIDs, cfg.Timezone, logger)

	// Wire the defense reminder callback to send inline keyboard buttons.
	sched.DefenseCallback = handler.SendDefenseReminderWithKeyboard

	sched.Start()

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("bot starting…", "timezone", cfg.Timezone, "chats", cfg.ChatIDs)
	go tgAdapter.Start(ctx)

	<-ctx.Done()
	logger.Info("shutting down…")
	sched.Stop()
	logger.Info("bye 👋")
}
