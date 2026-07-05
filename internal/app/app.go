package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"randomreviewer/internal/config"
	"randomreviewer/internal/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
	botgolang "github.com/mail-ru-im/bot-golang"
)

type Bot struct {
	ctx context.Context
	bot *botgolang.Bot
	wg  sync.WaitGroup
}

func New(ctx context.Context, cfg *config.Config) (*Bot, error) {
	app := new(Bot)
	bot, err := botgolang.NewBot(cfg.Bot.Token, botgolang.BotApiURL(cfg.Bot.ApiURL), botgolang.BotDebug(true))
	if err != nil {
		return nil, fmt.Errorf("new bot: %w", err)
	}
	app.bot = bot
	app.ctx = ctx
	slog.Info("bot started")

	conn, err := sql.Open("pgx", cfg.Postgres.DSN())
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	slog.Info("connected to postgres")

	app.wg.Go(func() {
		<-ctx.Done()
		if err := conn.Close(); err != nil {
			slog.Warn("failed to close postgres connection", "error", err)
		}
	})

	if err := migrations.Run(conn); err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	slog.Info("applied migrations")

	return app, nil
}

func matchPartType(botUserID string) func(part botgolang.Part) bool {
	return func(part botgolang.Part) bool {
		return part.Type == botgolang.MENTION && botUserID == part.Payload.UserID
	}
}

func (b *Bot) Start() {
	for update := range b.bot.GetUpdatesChannel(b.ctx) {
		if slices.ContainsFunc(update.Payload.Parts, matchPartType(b.bot.Info.ID)) {
			members, err := b.bot.GetChatMembers(update.Payload.Chat.ID)
			if err != nil {
				slog.Error("get chat members error:", err)
			}

			var me botgolang.ChatMember
			for _, m := range members {
				if m.User.ID != b.bot.Info.ID {
					me = m
				}
			}

			if err := update.Payload.Message().Reply(fmt.Sprintf("@[%s], ревью плиз", me.User.ID)); err != nil {
				slog.Error("send message error:", err)
			}
		}
	}

	b.wg.Wait()
}
