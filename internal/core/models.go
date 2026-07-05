package core

import "time"

type (
	UserID    string
	ChatID    string
	MessageID string
)

type Reviewer struct {
	ID         UserID
	ChatID     ChatID
	Weight     int
	FreezeTime time.Time
}

type Review struct {
	ID             int64
	ReviewerID     UserID
	MessageID      MessageID
	PrevReviewerID *UserID
}

type ResetType string

const (
	ResetTypeDay   ResetType = "day"
	ResetTypeWeek  ResetType = "week"
	ResetTypeMonth ResetType = "month"
)

type Chat struct {
	ID    ChatID
	Reset ResetType
}
