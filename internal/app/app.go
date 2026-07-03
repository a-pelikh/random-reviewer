package app

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"randomreviewer/internal/config"

	botgolang "github.com/mail-ru-im/bot-golang"
)

type Bot struct {
	bot *botgolang.Bot
}

func New(cfg *config.Bot) (*Bot, error) {
	app := new(Bot)
	bot, err := botgolang.NewBot(cfg.Token, botgolang.BotApiURL(cfg.ApiURL))
	if err != nil {
		return nil, fmt.Errorf("new bot: %w", err)
	}
	app.bot = bot

	return app, nil
}

func matchPartType(botUserID string) func(part botgolang.Part) bool {
	return func(part botgolang.Part) bool {
		return part.Type == botgolang.MENTION && botUserID == part.Payload.UserID
	}
}

func (b *Bot) Start(ctx context.Context) {
	for update := range b.bot.GetUpdatesChannel(ctx) {
		if slices.ContainsFunc(update.Payload.Parts, matchPartType(b.bot.Info.ID)) {
			members, err := b.bot.GetChatMembers(update.Payload.Chat.ID)
			if err != nil {
				slog.Error("get chat members error:", err)
			}
			slog.Info("left:", update.Payload.LeftMembers, "new", update.Payload.NewMembers)
			var me botgolang.ChatMember
			for _, m := range members {
				if m.User.ID != b.bot.Info.ID {
					me = m
				}
			}

			if err := update.Payload.Message().Reply(fmt.Sprintf("%s, ревью плиз", me.User.ID)); err != nil {
				slog.Error("send message error:", err)
			}
		}
	}
}
