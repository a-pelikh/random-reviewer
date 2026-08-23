package core

import "time"

type (
	ReviewID   int64
	ReviewerID int64
	UserID     string
	ChatID     string
	MessageID  string
)

type Reviewer struct {
	ID         ReviewerID
	UserID     UserID
	ChatID     ChatID
	Weight     int
	FreezeTime time.Time
	IsDeleted  bool
}

type Review struct {
	ID         ReviewID
	ReviewerID ReviewerID
	OwnerID    UserID
	MessageID  MessageID
	CreatedAt  time.Time
}

type ReviewMessage struct {
	ReviewID   ReviewID
	ReviewerID ReviewerID
	MessageID  MessageID
}

type Chat struct {
	ID        ChatID
	Reset     int
	LastReset time.Time
}
