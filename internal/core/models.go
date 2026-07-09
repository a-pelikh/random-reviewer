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
	ID            int64
	ReviewerID    UserID // empty = anchor record (M0/M1), no reviewer assigned
	ChatID        ChatID
	MessageID     MessageID
	PrevMessageID *MessageID
	RootMessageID MessageID // first message in the chain (M0)
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
