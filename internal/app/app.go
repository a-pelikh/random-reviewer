package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

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
	dateLayout = "02.01.2006"

	addCommand      = "add"
	removeCommand   = "remove"
	helpCommand     = "help"
	listCommand     = "list"
	freezeCommand   = "freeze"
	unfreezeCommand = "unfreeze"
	resetCommand    = "reset"

	helpText = `Команды:
• @bot – выбрать ревьюера
• @bot add @user – добавить ревьюера
• @bot remove @user – удалить ревьюера
• @bot help – список команд
• @bot list – список ревьюеров
• @bot reset <days> – устанавливает значение раз в сколько дней сбрасывается вес (раз в 14 дней по умолчанию)
• @bot freeze @user <date> –  замораживает ревьюера до переданной даты включительно (формат даты: dd.mm.yyyy)
• @bot unfreeze @user –  досрочно размораживает ревьюера
`
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

	service := random_reviewer.New(repository)
	app.service = service

	app.wg.Go(func() { service.Reset(ctx) })
	app.wg.Go(func() { service.Clean(ctx) })
	app.wg.Go(func() { service.ResetWeights(ctx) })

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

func getReplyMsgID(parts []botgolang.Part) (string, bool) {
	for _, part := range parts {
		if part.Type == botgolang.REPLY {
			return part.Payload.PartMessage.MsgID, true
		}
	}
	return "", false
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
				default:
					_ = reply(update.Payload.Message(), "Бот не может обработать ваше сообщение")
				}
			}
		}
	}

	b.wg.Wait()
}

func (b *Bot) matchCommand(payload botgolang.EventPayload) error {
	texts := strings.Fields(payload.Message().Text)
	switch {
	case slices.Contains(texts, helpCommand):
		return reply(payload.Message(), helpText)
	case slices.Contains(texts, listCommand):
		return b.list(payload)
	case slices.Contains(texts, addCommand):
		return b.add(payload)
	case slices.Contains(texts, removeCommand):
		return b.remove(payload)
	case slices.Contains(texts, resetCommand):
		return b.setReset(payload, texts)
	case slices.Contains(texts, freezeCommand):
		return b.freeze(payload, texts)
	case slices.Contains(texts, unfreezeCommand):
		return b.unfreeze(payload)
	default:
		return b.assign(payload)
	}
}

func (b *Bot) assign(payload botgolang.EventPayload) error {
	chatID := core.ChatID(payload.Chat.ID)
	ownerID := core.UserID(payload.From.ID)

	var repliedMessageIDs []core.MessageID
	if replyMsgID, ok := getReplyMsgID(payload.Parts); ok {
		repliedMessageIDs = append(repliedMessageIDs, core.MessageID(replyMsgID))
	}

	userID, reviewID, err := b.service.AssignReviewer(b.ctx, chatID, ownerID, repliedMessageIDs...)
	if err != nil {
		return fmt.Errorf("assign reviewer: %w", err)
	}

	msg := payload.Message()
	if err := reply(msg, fmt.Sprintf("@[%s], ревью плиз", userID)); err != nil {
		return fmt.Errorf("reply: %w", err)
	}

	oldMessageID, err := b.service.SetMessageID(b.ctx, reviewID, core.MessageID(msg.ID))
	if err != nil {
		slog.Error("failed to set message id", "error", err)
		return nil
	}

	if oldMessageID != "" {
		oldMsg := b.bot.NewMessage(payload.Chat.ID)
		oldMsg.ID = string(oldMessageID)
		if err := oldMsg.Delete(); err != nil {
			slog.Error("failed to delete old message", "error", err)
		}
	}

	return nil
}

func (b *Bot) list(payload botgolang.EventPayload) error {
	reviewers, err := b.service.GetReviewers(b.ctx, core.ChatID(payload.Chat.ID))
	if err != nil {
		return fmt.Errorf("get reviewers: %w", err)
	}

	if len(reviewers) == 0 {
		return reply(payload.Message(), "Список ревьюеров пуст")
	}

	var sb strings.Builder
	sb.WriteString("Список ревьюеров:\n")
	for _, reviewer := range reviewers {
		sb.WriteString(fmt.Sprintf("• @[%s] — вес: %d", reviewer.UserID, reviewer.Weight))
		if reviewer.FreezeTime.After(time.Now()) {
			sb.WriteString(fmt.Sprintf(", заморожен(а) до %s", reviewer.FreezeTime.Format(dateLayout)))
		}
		sb.WriteString("\n")
	}

	return reply(payload.Message(), sb.String())
}

func (b *Bot) add(payload botgolang.EventPayload) error {
	userID, err := getUserIDByMention(payload.Parts, b.bot.Info.ID)
	if err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}

	if err = b.service.AddReviewer(b.ctx, core.Reviewer{
		UserID: userID,
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
		UserID: userID,
		ChatID: core.ChatID(payload.Chat.ID),
	}); err != nil {
		return fmt.Errorf("remove reviewer: %w", err)
	}

	return reply(payload.Message(), fmt.Sprintf("@[%s], вы удалены из списка ревьюеров", userID))
}

func (b *Bot) setReset(payload botgolang.EventPayload, texts []string) error {
	days, err := parseIntArg(texts)
	if err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}

	if err := b.service.SetReset(b.ctx, core.ChatID(payload.Chat.ID), days); err != nil {
		return fmt.Errorf("set reset: %w", err)
	}

	return reply(payload.Message(), fmt.Sprintf("Вес ревьюеров теперь сбрасывается раз в %d дней", days))
}

func (b *Bot) freeze(payload botgolang.EventPayload, texts []string) error {
	userID, err := getUserIDByMention(payload.Parts, b.bot.Info.ID)
	if err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}

	date, err := parseDateArg(texts)
	if err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}

	if err := b.service.Freeze(b.ctx, core.Reviewer{
		UserID: userID,
		ChatID: core.ChatID(payload.Chat.ID),
	}, date); err != nil {
		return fmt.Errorf("freeze reviewer: %w", err)
	}

	return reply(payload.Message(), fmt.Sprintf("@[%s], заморожен(а) до %s включительно", userID, date.Format(dateLayout)))
}

func (b *Bot) unfreeze(payload botgolang.EventPayload) error {
	userID, err := getUserIDByMention(payload.Parts, b.bot.Info.ID)
	if err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}

	if err := b.service.Unfreeze(b.ctx, core.Reviewer{
		UserID: userID,
		ChatID: core.ChatID(payload.Chat.ID),
	}); err != nil {
		return fmt.Errorf("unfreeze reviewer: %w", err)
	}

	return reply(payload.Message(), fmt.Sprintf("@[%s], разморожен(а)", userID))
}

func parseIntArg(texts []string) (int, error) {
	for _, text := range texts {
		if n, err := strconv.Atoi(text); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("no integer argument found")
}

func parseDateArg(texts []string) (time.Time, error) {
	for _, text := range texts {
		if date, err := time.Parse(dateLayout, text); err == nil {
			return date, nil
		}
	}
	return time.Time{}, fmt.Errorf("no date argument found")
}
