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
	"randomreviewer/internal/repository/fs"
	"randomreviewer/internal/repository/postgres"
	random_reviewer "randomreviewer/internal/service/random-reviewer"

	_ "github.com/jackc/pgx/v5/stdlib"
	botgolang "github.com/mail-ru-im/bot-golang"
)

const (
	addCommand    = "add"
	removeCommand = "remove"
	helpCommand   = "help"

	helpText = `Команды:
• @bot – выбрать ревьюера
• @bot add @user – добавить ревьюера
• @bot remove @user – удалить ревьюера
• @bot help – список команд`
)

type Bot struct {
	ctx context.Context
	bot *botgolang.Bot
	wg  sync.WaitGroup

	service core.ReviewersService
}

func New(ctx context.Context, cfg *config.Config) (*Bot, error) {
	app := new(Bot)
	bot, err := botgolang.NewBot(cfg.Bot.Token, botgolang.BotApiURL(cfg.Bot.ApiURL))
	if err != nil {
		return nil, fmt.Errorf("new bot: %w", err)
	}
	app.bot = bot
	app.ctx = ctx
	slog.Info("bot started")

	var repository core.ReviewersRepository
	if cfg.Storage.Type == "fs" {
		path := cfg.Storage.Path
		if path == "" {
			path = "data.json"
		}
		repository = fs.New(path)
		slog.Info("using fs storage", "path", path)
	} else {
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

		repository = postgres.New(conn)
	}

	service, err := random_reviewer.New(repository, cfg.Bot.Secret)
	if err != nil {
		return nil, fmt.Errorf("new service: %w", err)
	}
	app.service = service
	//app.wg.Go(func() {
	//	if err := app.service.Reset(ctx); err != nil {
	//		slog.Warn("failed to reset chat", "error", err)
	//	}
	//})

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

func reply(message *botgolang.Message, text string) error {
	if err := message.Reply(text); err != nil {
		slog.Error("failed to reply", "error", err)
		return err
	}
	return nil
}

func (b *Bot) Start() {
	for update := range b.bot.GetUpdatesChannel(b.ctx) {
		if slices.ContainsFunc(update.Payload.Parts, matchPartTypeWithBotUserIDMention(b.bot.Info.ID)) {
			if err := b.matchCommand(update.Payload); err != nil {
				slog.Error("match command", "payload", update.Payload, "error", err)
				switch {
				case errors.Is(err, core.ErrNoReviewersAvailable):
					_ = reply(update.Payload.Message(), "Список ревьюеров пуст")
				case errors.Is(err, core.ErrNoAnotherReviewersAllowed):
					_ = reply(update.Payload.Message(), "Нет другого доступного ревьюера для реролла")
				case errors.Is(err, core.ErrNoUserMentioned):
					_ = reply(update.Payload.Message(), "Не указан пользователь для выполнения команды")
				case errors.Is(err, core.ErrUserAlreadyAdded):
					_ = reply(update.Payload.Message(), "Пользователь уже является ревьюером в этом чате")
				case errors.Is(err, core.ErrUserNotInReviewersList):
					_ = reply(update.Payload.Message(), "Пользователя нет в списке ревьюеров")
				case errors.Is(err, core.ErrUnknowCommand):
					_ = reply(update.Payload.Message(), "Неизвестная команда, для отображения всех команд используйте команду help")
				case err != nil:
					_ = reply(update.Payload.Message(), "Бот не может обработать ваше сообщение")
				}
			}
		}
	}

	b.wg.Wait()
}

func (b *Bot) matchCommand(payload botgolang.EventPayload) error {
	slog.Info("matchCommand", "text", payload.Message().Text, "parts", payload.Parts)
	texts := strings.Fields(payload.Message().Text)
	switch {
	case slices.Contains(texts, helpCommand):
		return reply(payload.Message(), helpText)
	case slices.Contains(texts, addCommand):
		return b.add(payload)
	case slices.Contains(texts, removeCommand):
		return b.remove(payload)
	default:
		if replyMsgID, ok := getReplyMsgID(payload.Parts); ok {
			return b.handleReply(payload, core.MessageID(replyMsgID))
		}
		return b.getReviewer(payload)
	}
}

func (b *Bot) handleReply(payload botgolang.EventPayload, replyMsgID core.MessageID) error {
	requesterID := core.UserID(payload.From.ID)
	chatID := core.ChatID(payload.Chat.ID)

	nextUserID, rootMsgID, err := b.service.RerollReview(b.ctx, chatID, replyMsgID, requesterID)
	if errors.Is(err, core.ErrNotInChain) {
		return b.assignInitial(payload, replyMsgID)
	}
	if err != nil {
		return fmt.Errorf("reroll review: %w", err)
	}

	msg := payload.Message()
	if err = reply(msg, fmt.Sprintf("@[%s], ревью плиз", nextUserID)); err != nil {
		return fmt.Errorf("reply: %w", err)
	}

	botMsgID := core.MessageID(msg.ID)
	if err = b.service.AssignReviewer(b.ctx, core.Review{
		ReviewerID:    nextUserID,
		ChatID:        chatID,
		MessageID:     botMsgID,
		PrevMessageID: &replyMsgID,
		RootMessageID: rootMsgID,
	}); err != nil {
		slog.Error("failed to assign reviewer", "error", err)
	}

	return nil
}

// assignInitial selects a reviewer for the first time, stores the trigger message
// as a null-reviewer anchor and the bot response as the actual review.
// rootMsgID is the root of the chain (M0): for standalone calls it equals the trigger
// message ID; for reply-to-M0 calls it is the replied-to message ID.
func (b *Bot) assignInitial(payload botgolang.EventPayload, rootMsgID core.MessageID) error {
	chatID := core.ChatID(payload.Chat.ID)

	userID, err := b.service.GetReviewer(b.ctx, chatID, core.UserID(payload.From.ID))
	if err != nil {
		return fmt.Errorf("get reviewer: %w", err)
	}

	msg := payload.Message()
	triggerMsgID := core.MessageID(msg.ID) // M0 (standalone) or M1 (reply case)

	if err = reply(msg, fmt.Sprintf("@[%s], ревью плиз", userID)); err != nil {
		return fmt.Errorf("reply: %w", err)
	}

	botMsgID := core.MessageID(msg.ID) // M2 (bot response, after reply)

	if err = b.service.AssignReviewer(b.ctx, core.Review{
		ChatID:        chatID,
		MessageID:     triggerMsgID,
		RootMessageID: rootMsgID,
	}); err != nil {
		slog.Error("failed to store anchor", "error", err)
	}

	if err = b.service.AssignReviewer(b.ctx, core.Review{
		ReviewerID:    userID,
		ChatID:        chatID,
		MessageID:     botMsgID,
		PrevMessageID: &triggerMsgID,
		RootMessageID: rootMsgID,
	}); err != nil {
		slog.Error("failed to assign reviewer", "error", err)
	}

	return nil
}

func (b *Bot) getReviewer(payload botgolang.EventPayload) error {
	rootMsgID := core.MessageID(payload.Message().ID)
	return b.assignInitial(payload, rootMsgID)
}

func getReplyMsgID(parts []botgolang.Part) (string, bool) {
	for _, part := range parts {
		if part.Type == botgolang.REPLY {
			return part.Payload.PartMessage.MsgID, true
		}
	}
	return "", false
}

func (b *Bot) add(payload botgolang.EventPayload) error {
	userID, err := getUserIDByMention(payload.Parts, b.bot.Info.ID)
	if err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}

	if err = b.service.AddReviewer(b.ctx, core.Reviewer{
		ID:     userID,
		ChatID: core.ChatID(payload.Chat.ID),
	}); err != nil {
		return fmt.Errorf("add reviewer: %w", err)
	}

	return reply(payload.Message(), fmt.Sprintf("@[%s], вы добавлены в список ревьюеров", userID))
}

func (b *Bot) remove(payload botgolang.EventPayload) error {
	userID, err := getUserIDByMention(payload.Parts, b.bot.Info.ID)
	if err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}

	if err = b.service.RemoveReviewer(b.ctx, core.Reviewer{
		ID:     userID,
		ChatID: core.ChatID(payload.Chat.ID),
	}); err != nil {
		return fmt.Errorf("remove reviewer: %w", err)
	}

	return reply(payload.Message(), fmt.Sprintf("@[%s], вы удалены из списка ревьюеров", userID))
}
