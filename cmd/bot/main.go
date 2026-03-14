package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/BinaryArchaism/polymarket-go-bot/internal/config"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/engine"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/exchange/polymarket"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/notify/telegram"
	"github.com/BinaryArchaism/polymarket-go-bot/internal/storage/postgres"

	"github.com/rs/zerolog"
)

func main() {
	cfg, err := config.Load(os.Getenv("CONFIG_PATH"))
	if err != nil {
		os.Stderr.WriteString("failed to load config: " + err.Error() + "\n")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Telegram notifier
	tgNotifier := telegram.NewNotifier(
		cfg.Telegram.BotToken,
		cfg.Telegram.ChatID,
		cfg.Telegram.Enabled,
		zerolog.New(os.Stderr).With().Timestamp().Logger(),
	)

	// Logger with multi-writer
	var writers []io.Writer
	writers = append(writers, zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: "15:04:05"})

	if cfg.Telegram.Enabled {
		tgWriter := telegram.NewTelegramWriter(ctx, tgNotifier, zerolog.InfoLevel)
		writers = append(writers, tgWriter)
	}

	multiWriter := io.MultiWriter(writers...)
	log := zerolog.New(multiWriter).With().Timestamp().Logger()

	if cfg.Telegram.Enabled {
		tgNotifier.SendInfo(ctx, "Polybot started")
	}

	// Storage
	stg, err := postgres.New(ctx, log, *cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize storage")
	}

	// Exchange client
	var opts []polymarket.ClientOption
	if cfg.Market.MockOrders {
		opts = append(opts, polymarket.MockOrder)
	}
	cli, err := polymarket.New(*cfg, opts...)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize polymarket client")
	}

	// Engine
	eng := engine.NewEngine(log, *cfg, cli, stg)
	eng.Start(ctx)

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Info().Msg("shutdown signal received")

	if cfg.Telegram.Enabled {
		tgNotifier.SendInfo(ctx, "Polybot shutting down")
	}

	cancel()
	eng.Stop()

	log.Info().Msg("polybot shutdown complete")
}
