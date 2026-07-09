package core

import "context"

type ReviewersService interface {
	AddReviewer(ctx context.Context, reviewer Reviewer) error
	GetReviewer(ctx context.Context, chatID ChatID, requesterID UserID) (UserID, error)
	RerollReview(ctx context.Context, chatID ChatID, replyMsgID MessageID, requesterID UserID) (UserID, MessageID, error)
	AssignReviewer(ctx context.Context, review Review) error
	RemoveReviewer(ctx context.Context, reviewer Reviewer) error
	SetReset(ctx context.Context, chat Chat) error
	Reset(ctx context.Context) error
}

type ReviewersRepository interface {
	AddReviewer(ctx context.Context, reviewer Reviewer) error
	RemoveReviewer(ctx context.Context, reviewer Reviewer) error
	GetAvailableReviewers(ctx context.Context, chatID ChatID) ([]Reviewer, error)
	AssignReviewer(ctx context.Context, review Review) error
	// GetChainReviewers finds the root message and all reviewer IDs for that chain.
	// messageID can be any message in the chain (message_id or root_message_id).
	GetChainReviewers(ctx context.Context, messageID MessageID) (rootMessageID MessageID, reviewerIDs []UserID, err error)
}
