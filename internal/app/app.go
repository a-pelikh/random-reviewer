package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"randomreviewer/internal/config"
	"randomreviewer/internal/core"
	"randomreviewer/internal/migrations"
	"randomreviewer/internal/repository/postgres"
	"randomreviewer/internal/service/random-reviewer"

	_ "github.com/jackc/pgx/v5/stdlib"
	botgolang "github.com/mail-ru-im/bot-golang"
)

const (
	addCommand = "add"
)

type Bot struct {
	ctx context.Context
	bot *botgolang.Bot
	wg  sync.WaitGroup

	service core.ReviewersService
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

	migConn, err := sql.Open("pgx", cfg.Postgres.DSN())
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	if err := migConn.Ping(); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	if err := migrations.Run(migConn); err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	slog.Info("applied migrations")

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

	repository := postgres.New(conn)

	app.service = random_reviewer.New(repository)
	return app, nil
}

func matchPartTypeWithBotUserIDMention(botUserID string) func(part botgolang.Part) bool {
	return func(part botgolang.Part) bool {
		return part.Type == botgolang.MENTION && botUserID == part.Payload.UserID
	}
}

func getUserIDByMention(parts []botgolang.Part, botUserID string) (core.UserID, error) {
	for _, part := range parts {
		if part.Type == botgolang.MENTION && botUserID != part.Payload.UserID {
			return core.UserID(part.Payload.UserID), nil
		}
	}

	var zero core.UserID
	return zero, core.ErrNoUserMentioned
}

func reply(message *botgolang.Message, text string) {
	if err := message.Reply(text); err != nil {
		slog.Error("failed to reply", "error", err)
	}
}

func (b *Bot) Start() {
	for update := range b.bot.GetUpdatesChannel(b.ctx) {
		if slices.ContainsFunc(update.Payload.Parts, matchPartTypeWithBotUserIDMention(b.bot.Info.ID)) {
			if err := b.matchCommand(update.Payload); err != nil {
				slog.Error("match command", "payload", update.Payload, "error", err)
				switch {
				case errors.Is(err, core.ErrNoUserMentioned):
					reply(update.Payload.Message(), "Не указан пользователь для добавления в ревьюеры")
				case errors.Is(err, core.ErrUserAlreadyAdded):
					reply(update.Payload.Message(), "Пользователь уже является ревьюером в этом чате")
				case err != nil:
					reply(update.Payload.Message(), "Бот не может обработать ваше сообщение")
				}
			}
			//if err := update.Payload.Message().Reply(fmt.Sprintf("@[%s], ревью плиз", me.User.ID)); err != nil {
			//	slog.Error("send message error:", err)
			//}
		}
	}

	b.wg.Wait()
}

func (b *Bot) matchCommand(payload botgolang.EventPayload) error {
	texts := strings.Split(payload.Message().Text, " ")
	switch {
	case slices.Contains(texts, addCommand):
		userID, err := getUserIDByMention(payload.Parts, b.bot.Info.ID)
		if err != nil {
			return fmt.Errorf("invalid command: %w", err)
		}

		err = b.service.AddReviewer(b.ctx, core.Reviewer{
			ID:     userID,
			ChatID: core.ChatID(payload.Chat.ID),
		})
		if err != nil {
			return fmt.Errorf("add reviewer: %w", err)
		}

		reply(payload.Message(), fmt.Sprintf("@[%s], вы успешно добавлены в ревьюеры", userID))
	}

	return nil
}
