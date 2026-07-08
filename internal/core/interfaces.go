package core

import "context"

type ReviewersService interface {
	AddReviewer(ctx context.Context, reviewer Reviewer) error
	RerollLastReviewer(ctx context.Context, chatID ChatID, messageID MessageID, requesterID UserID) (UserID, UserID, error)
	GetReviewer(ctx context.Context, chatID ChatID, requesterID UserID) (UserID, error)
	AssignReviewer(ctx context.Context, review Review) error
	RemoveReviewer(ctx context.Context, reviewer Reviewer) error
	SetReset(ctx context.Context, chat Chat) error
	Reset(ctx context.Context) error
}

type ReviewersRepository interface {
	AddReviewer(ctx context.Context, reviewer Reviewer) error
	RemoveReviewer(ctx context.Context, reviewer Reviewer) error
	GetAvailableReviewers(ctx context.Context, chatID ChatID) ([]Reviewer, error)
	GetActualReviewer(ctx context.Context, messageID MessageID) (UserID, error)
	AssignReviewer(ctx context.Context, review Review) error
}
