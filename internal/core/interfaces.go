package core

import (
	"context"
	"time"
)

type ReviewersService interface {
	GetReviewers(ctx context.Context, chatID ChatID) ([]Reviewer, error)
	AddReviewer(ctx context.Context, reviewer Reviewer) error
	AssignReviewer(ctx context.Context, chatID ChatID, ownerID UserID, repliedMessages ...MessageID) (UserID, ReviewID, error)
	SetMessageID(ctx context.Context, reviewID ReviewID, messageID MessageID) (MessageID, error)
	RemoveReviewer(ctx context.Context, reviewer Reviewer) error
	SetReset(ctx context.Context, chatID ChatID, reset int) error
	Reset(ctx context.Context)
	Clean(ctx context.Context)
	Freeze(ctx context.Context, reviewer Reviewer, date time.Time) error
	Unfreeze(ctx context.Context, reviewer Reviewer) error
	ResetWeights(ctx context.Context)
}

type ReviewersRepository interface {
	GetReviewers(ctx context.Context, chatID ChatID) ([]Reviewer, error)
	AddReviewer(ctx context.Context, reviewer Reviewer) error
	GetReview(ctx context.Context, messageID MessageID) (Review, error)
	AssignReviewer(ctx context.Context, review Review) (ReviewID, error)
	SaveReviewMessages(ctx context.Context, reviewID ReviewID, reviewerID ReviewerID, messageID ...MessageID) error
	RerollReviewer(ctx context.Context, newReviewerID ReviewerID, reviewID ReviewID) error
	SetMessageID(ctx context.Context, reviewID ReviewID, messageID MessageID) (MessageID, error)
	GetAvailableReviewers(ctx context.Context, review ReviewID) ([]Reviewer, error)
	RemoveReviewer(ctx context.Context, reviewer Reviewer) error
	SetReset(ctx context.Context, chatID ChatID, reset int) error
	Reset(ctx context.Context) error
	Clean(ctx context.Context) error
	Unfreeze(ctx context.Context, reviewer Reviewer) error
	Freeze(ctx context.Context, reviewer Reviewer, date time.Time) error
	ResetWeights(ctx context.Context) error
}
