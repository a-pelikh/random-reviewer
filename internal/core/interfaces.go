package core

import "context"

type ReviewersService interface {
	UpdateActualChatMembers(ctx context.Context, chatID string, chatMembers []ChatMember) error
	RerollLastReviewer(ctx context.Context, chatID string, userID ChatMember) (ChatMember, error)
	GetReviewer(ctx context.Context, chatID string) (ChatMember, error)
}

type ReviewersRepository interface {
}
