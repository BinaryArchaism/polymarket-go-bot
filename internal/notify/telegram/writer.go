package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type TelegramWriter struct {
	notifier   *Notifier
	minLevel   zerolog.Level
	ctx        context.Context
	throttle   time.Duration
	lastSent   time.Time
	maxMsgLen  int
}

func NewTelegramWriter(ctx context.Context, notifier *Notifier, minLevel zerolog.Level) *TelegramWriter {
	return &TelegramWriter{
		notifier:  notifier,
		minLevel:  minLevel,
		ctx:       ctx,
		throttle:  5 * time.Second,
		maxMsgLen: 4000,
	}
}

func (w *TelegramWriter) Write(p []byte) (n int, err error) {
	if !w.notifier.enabled {
		return len(p), nil
	}

	msg := string(p)

	level := w.extractLevel(msg)
	if level < w.minLevel {
		return len(p), nil
	}

	if time.Since(w.lastSent) < w.throttle {
		return len(p), nil
	}

	w.lastSent = time.Now()

	if len(msg) > w.maxMsgLen {
		msg = msg[:w.maxMsgLen] + "\n... (truncated)"
	}

	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	go func() {
		if sendErr := w.notifier.SendMessage(ctx, msg); sendErr != nil {
		}
	}()

	return len(p), nil
}

func (w *TelegramWriter) extractLevel(msg string) zerolog.Level {
	msg = strings.ToLower(msg)

	if strings.Contains(msg, `"level":"fatal"`) || strings.Contains(msg, `"lvl":"ftl"`) {
		return zerolog.FatalLevel
	}
	if strings.Contains(msg, `"level":"error"`) || strings.Contains(msg, `"lvl":"err"`) {
		return zerolog.ErrorLevel
	}
	if strings.Contains(msg, `"level":"warn"`) || strings.Contains(msg, `"lvl":"wrn"`) {
		return zerolog.WarnLevel
	}
	if strings.Contains(msg, `"level":"info"`) || strings.Contains(msg, `"lvl":"inf"`) {
		return zerolog.InfoLevel
	}
	if strings.Contains(msg, `"level":"debug"`) || strings.Contains(msg, `"lvl":"dbg"`) {
		return zerolog.DebugLevel
	}

	return zerolog.NoLevel
}

func (w *TelegramWriter) WriteLevel(level string, p []byte) (n int, err error) {
	msg := string(p)

	var emoji string
	switch level {
	case "fatal", "panic":
		emoji = "🔴"
	case "error":
		emoji = "🔴"
	case "warn":
		emoji = "⚠️"
	case "info":
		emoji = "ℹ️"
	default:
		return len(p), nil
	}

	formattedMsg := fmt.Sprintf("%s [%s]\n%s", emoji, strings.ToUpper(level), msg)

	if len(formattedMsg) > w.maxMsgLen {
		formattedMsg = formattedMsg[:w.maxMsgLen] + "\n... (truncated)"
	}

	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	go func() {
		if sendErr := w.notifier.SendMessage(ctx, formattedMsg); sendErr != nil {
		}
	}()

	return len(p), nil
}
