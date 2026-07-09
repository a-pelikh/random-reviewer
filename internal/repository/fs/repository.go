package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"randomreviewer/internal/core"
)

type reviewerRecord struct {
	ID         core.UserID `json:"id"`
	ChatID     core.ChatID `json:"chat_id"`
	Weight     int         `json:"weight"`
	FreezeTime *time.Time  `json:"freeze_time,omitempty"`
	IsDeleted  bool        `json:"is_deleted"`
}

type reviewRecord struct {
	ReviewerID    core.UserID     `json:"reviewer_id,omitempty"`
	ChatID        core.ChatID     `json:"chat_id"`
	MessageID     core.MessageID  `json:"message_id"`
	PrevMessageID *core.MessageID `json:"prev_message_id,omitempty"`
	RootMessageID core.MessageID  `json:"root_message_id"`
}

type storage struct {
	Reviewers []reviewerRecord `json:"reviewers"`
	Reviews   []reviewRecord   `json:"reviews"`
}

type repositoryImpl struct {
	path string
	mu   sync.Mutex
}

func New(path string) core.ReviewersRepository {
	return &repositoryImpl{path: path}
}

func (r *repositoryImpl) load() (storage, error) {
	var s storage
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("read file: %w", err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("unmarshal: %w", err)
	}
	return s, nil
}

func (r *repositoryImpl) save(s storage) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(r.path, data, 0600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

func (r *repositoryImpl) AddReviewer(_ context.Context, reviewer core.Reviewer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	for i, rec := range s.Reviewers {
		if rec.ID == reviewer.ID && rec.ChatID == reviewer.ChatID {
			if !rec.IsDeleted {
				return core.ErrUserAlreadyAdded
			}
			s.Reviewers[i].IsDeleted = false
			return r.save(s)
		}
	}

	s.Reviewers = append(s.Reviewers, reviewerRecord{
		ID:     reviewer.ID,
		ChatID: reviewer.ChatID,
	})
	return r.save(s)
}

func (r *repositoryImpl) RemoveReviewer(_ context.Context, reviewer core.Reviewer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	for i, rec := range s.Reviewers {
		if rec.ID == reviewer.ID && rec.ChatID == reviewer.ChatID && !rec.IsDeleted {
			s.Reviewers[i].IsDeleted = true
			return r.save(s)
		}
	}

	return core.ErrUserNotInReviewersList
}

func (r *repositoryImpl) GetAvailableReviewers(_ context.Context, chatID core.ChatID) ([]core.Reviewer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var reviewers []core.Reviewer
	for _, rec := range s.Reviewers {
		if rec.ChatID != chatID || rec.IsDeleted {
			continue
		}
		if rec.FreezeTime != nil && rec.FreezeTime.After(now) {
			continue
		}
		reviewers = append(reviewers, core.Reviewer{
			ID:     rec.ID,
			ChatID: rec.ChatID,
			Weight: rec.Weight,
		})
	}

	return reviewers, nil
}

func (r *repositoryImpl) GetChainReviewers(_ context.Context, messageID core.MessageID) (core.MessageID, []core.UserID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return "", nil, err
	}

	// Find the root: messageID can be a message_id or a root_message_id in reviews.
	var rootMsgID core.MessageID
	found := false
	for _, rec := range s.Reviews {
		if rec.MessageID == messageID {
			rootMsgID = rec.RootMessageID
			found = true
			break
		}
	}
	if !found {
		for _, rec := range s.Reviews {
			if rec.RootMessageID == messageID {
				rootMsgID = messageID
				found = true
				break
			}
		}
	}
	if !found {
		return "", nil, core.ErrNotInChain
	}

	var reviewerIDs []core.UserID
	for _, rec := range s.Reviews {
		if rec.RootMessageID == rootMsgID && rec.ReviewerID != "" {
			reviewerIDs = append(reviewerIDs, rec.ReviewerID)
		}
	}

	return rootMsgID, reviewerIDs, nil
}

func (r *repositoryImpl) AssignReviewer(_ context.Context, review core.Review) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	if review.ReviewerID != "" {
		for i, rec := range s.Reviewers {
			if rec.ChatID == review.ChatID && rec.ID == review.ReviewerID {
				s.Reviewers[i].Weight++
			}
		}

		if review.PrevMessageID != nil {
			for _, rec := range s.Reviews {
				if rec.MessageID == *review.PrevMessageID && rec.ReviewerID != "" {
					for i, rev := range s.Reviewers {
						if rev.ChatID == review.ChatID && rev.ID == rec.ReviewerID && s.Reviewers[i].Weight > 0 {
							s.Reviewers[i].Weight--
						}
					}
					break
				}
			}
		}
	}

	s.Reviews = append(s.Reviews, reviewRecord{
		ReviewerID:    review.ReviewerID,
		ChatID:        review.ChatID,
		MessageID:     review.MessageID,
		PrevMessageID: review.PrevMessageID,
		RootMessageID: review.RootMessageID,
	})

	return r.save(s)
}
