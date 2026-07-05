package core

import "context"

type ReviewersService interface {
	AddReviewer(ctx context.Context, reviewer Reviewer) error
	RerollLastReviewer(ctx context.Context, chatID ChatID, messageID MessageID) (UserID, error)
	GetReviewer(ctx context.Context, chatID ChatID) (UserID, error)
	RemoveReviewer(ctx context.Context, reviewer Reviewer) error
	SetReset(ctx context.Context, chat Chat) error
}

type ReviewersRepository interface {
	AddReviewer(ctx context.Context, reviewer Reviewer) error
	RemoveReviewer(ctx context.Context, reviewer Reviewer) error
}
