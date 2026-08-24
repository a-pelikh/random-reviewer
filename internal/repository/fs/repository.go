package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"randomreviewer/internal/core"
)

const (
	defaultResetDays = 14
	cleanAfter       = 14 * 24 * time.Hour
)

type chatRecord struct {
	ChatID    core.ChatID `json:"chat_id"`
	ResetDays int         `json:"reset_days"`
	LastReset *time.Time  `json:"last_reset,omitempty"`
}

type reviewerRecord struct {
	ID         core.ReviewerID `json:"id"`
	UserID     core.UserID     `json:"user_id"`
	ChatID     core.ChatID     `json:"chat_id"`
	Weight     int             `json:"weight"`
	FreezeTime *time.Time      `json:"freeze_time,omitempty"`
	IsDeleted  bool            `json:"is_deleted"`
}

type reviewRecord struct {
	ID         core.ReviewID   `json:"id"`
	ReviewerID core.ReviewerID `json:"reviewer_id"`
	OwnerID    core.UserID     `json:"owner_id"`
	MessageID  core.MessageID  `json:"message_id,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

type reviewMessageRecord struct {
	ReviewID   core.ReviewID   `json:"review_id"`
	ReviewerID core.ReviewerID `json:"reviewer_id"`
	MessageID  core.MessageID  `json:"message_id"`
}

type storage struct {
	Chats          []chatRecord          `json:"chats"`
	Reviewers      []reviewerRecord      `json:"reviewers"`
	Reviews        []reviewRecord        `json:"reviews"`
	ReviewMessages []reviewMessageRecord `json:"review_messages"`
	NextReviewerID int64                 `json:"next_reviewer_id"`
	NextReviewID   int64                 `json:"next_review_id"`
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

func (r *repositoryImpl) ensureChat(s *storage, chatID core.ChatID) {
	for _, chat := range s.Chats {
		if chat.ChatID == chatID {
			return
		}
	}
	now := time.Now()
	s.Chats = append(s.Chats, chatRecord{ChatID: chatID, ResetDays: defaultResetDays, LastReset: &now})
}

func isFrozen(rec reviewerRecord, now time.Time) bool {
	return rec.FreezeTime != nil && rec.FreezeTime.After(now)
}

func (r *repositoryImpl) avgWeight(s storage, chatID core.ChatID) int {
	now := time.Now()
	var sum, count int
	for _, rec := range s.Reviewers {
		if rec.ChatID != chatID || rec.IsDeleted || isFrozen(rec, now) {
			continue
		}
		sum += rec.Weight
		count++
	}
	if count == 0 {
		return 0
	}
	return int(math.Ceil(float64(sum) / float64(count)))
}

func (r *repositoryImpl) incrementWeight(s *storage, reviewerID core.ReviewerID) {
	for i, rec := range s.Reviewers {
		if rec.ID == reviewerID {
			s.Reviewers[i].Weight++
			return
		}
	}
}

func (r *repositoryImpl) decrementWeight(s *storage, reviewerID core.ReviewerID) {
	for i, rec := range s.Reviewers {
		if rec.ID == reviewerID && rec.Weight > 0 {
			s.Reviewers[i].Weight--
			return
		}
	}
}

func (r *repositoryImpl) GetReviewers(_ context.Context, chatID core.ChatID) ([]core.Reviewer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return nil, err
	}

	var reviewers []core.Reviewer
	for _, rec := range s.Reviewers {
		if rec.ChatID != chatID || rec.IsDeleted {
			continue
		}
		rev := core.Reviewer{ID: rec.ID, UserID: rec.UserID, ChatID: rec.ChatID, Weight: rec.Weight}
		if rec.FreezeTime != nil {
			rev.FreezeTime = *rec.FreezeTime
		}
		reviewers = append(reviewers, rev)
	}

	return reviewers, nil
}

func (r *repositoryImpl) AddReviewer(_ context.Context, reviewer core.Reviewer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	r.ensureChat(&s, reviewer.ChatID)
	avg := r.avgWeight(s, reviewer.ChatID)
	now := time.Now()

	for i, rec := range s.Reviewers {
		if rec.UserID == reviewer.UserID && rec.ChatID == reviewer.ChatID {
			if !rec.IsDeleted || isFrozen(rec, now) {
				return core.ErrUserAlreadyAdded
			}
			s.Reviewers[i].IsDeleted = false
			s.Reviewers[i].Weight = avg
			return r.save(s)
		}
	}

	s.NextReviewerID++
	s.Reviewers = append(s.Reviewers, reviewerRecord{
		ID:     core.ReviewerID(s.NextReviewerID),
		UserID: reviewer.UserID,
		ChatID: reviewer.ChatID,
		Weight: avg,
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
		if rec.UserID == reviewer.UserID && rec.ChatID == reviewer.ChatID && !rec.IsDeleted {
			s.Reviewers[i].IsDeleted = true
			return r.save(s)
		}
	}

	return core.ErrUserNotInReviewersList
}

func (r *repositoryImpl) GetReview(_ context.Context, messageID core.MessageID) (core.Review, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return core.Review{}, err
	}

	var reviewID core.ReviewID
	found := false
	for _, rm := range s.ReviewMessages {
		if rm.MessageID == messageID {
			reviewID = rm.ReviewID
			found = true
			break
		}
	}
	if !found {
		return core.Review{}, core.ErrReviewNotFound
	}

	for _, rec := range s.Reviews {
		if rec.ID == reviewID {
			return core.Review{
				ID:         rec.ID,
				ReviewerID: rec.ReviewerID,
				OwnerID:    rec.OwnerID,
				MessageID:  rec.MessageID,
				CreatedAt:  rec.CreatedAt,
			}, nil
		}
	}

	return core.Review{}, core.ErrReviewNotFound
}

func (r *repositoryImpl) AssignReviewer(_ context.Context, review core.Review) (core.ReviewID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return 0, err
	}

	s.NextReviewID++
	reviewID := core.ReviewID(s.NextReviewID)
	s.Reviews = append(s.Reviews, reviewRecord{
		ID:         reviewID,
		ReviewerID: review.ReviewerID,
		OwnerID:    review.OwnerID,
		CreatedAt:  time.Now(),
	})

	r.incrementWeight(&s, review.ReviewerID)

	if err := r.save(s); err != nil {
		return 0, err
	}
	return reviewID, nil
}

func (r *repositoryImpl) SaveReviewMessages(_ context.Context, reviewID core.ReviewID, reviewerID core.ReviewerID, messageIDs ...core.MessageID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	for _, messageID := range messageIDs {
		s.ReviewMessages = append(s.ReviewMessages, reviewMessageRecord{
			ReviewID:   reviewID,
			ReviewerID: reviewerID,
			MessageID:  messageID,
		})
	}

	return r.save(s)
}

func (r *repositoryImpl) RerollReviewer(_ context.Context, newReviewerID core.ReviewerID, reviewID core.ReviewID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	var oldReviewerID core.ReviewerID
	found := false
	for i, rec := range s.Reviews {
		if rec.ID == reviewID {
			oldReviewerID = rec.ReviewerID
			s.Reviews[i].ReviewerID = newReviewerID
			found = true
			break
		}
	}
	if !found {
		return core.ErrReviewNotFound
	}

	r.incrementWeight(&s, newReviewerID)
	r.decrementWeight(&s, oldReviewerID)

	return r.save(s)
}

func (r *repositoryImpl) SetMessageID(_ context.Context, reviewID core.ReviewID, messageID core.MessageID) (core.MessageID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return "", err
	}

	var oldMessageID core.MessageID
	var reviewerID core.ReviewerID
	found := false
	for i, rec := range s.Reviews {
		if rec.ID == reviewID {
			oldMessageID = rec.MessageID
			s.Reviews[i].MessageID = messageID
			reviewerID = rec.ReviewerID
			found = true
			break
		}
	}
	if !found {
		return "", core.ErrReviewNotFound
	}

	s.ReviewMessages = append(s.ReviewMessages, reviewMessageRecord{
		ReviewID:   reviewID,
		ReviewerID: reviewerID,
		MessageID:  messageID,
	})

	if err := r.save(s); err != nil {
		return "", err
	}
	return oldMessageID, nil
}

func (r *repositoryImpl) GetAvailableReviewers(_ context.Context, reviewID core.ReviewID) ([]core.Reviewer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return nil, err
	}

	var review *reviewRecord
	for i, rec := range s.Reviews {
		if rec.ID == reviewID {
			review = &s.Reviews[i]
			break
		}
	}
	if review == nil {
		return nil, core.ErrReviewNotFound
	}

	var chatID core.ChatID
	for _, rec := range s.Reviewers {
		if rec.ID == review.ReviewerID {
			chatID = rec.ChatID
			break
		}
	}

	used := make(map[core.ReviewerID]bool)
	for _, rm := range s.ReviewMessages {
		if rm.ReviewID == reviewID {
			used[rm.ReviewerID] = true
		}
	}

	now := time.Now()
	var reviewers []core.Reviewer
	for _, rec := range s.Reviewers {
		if rec.ChatID != chatID || rec.IsDeleted || isFrozen(rec, now) {
			continue
		}
		if rec.UserID == review.OwnerID || used[rec.ID] {
			continue
		}
		reviewers = append(reviewers, core.Reviewer{ID: rec.ID, UserID: rec.UserID, Weight: rec.Weight})
	}

	return reviewers, nil
}

func (r *repositoryImpl) SetReset(_ context.Context, chatID core.ChatID, reset int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	r.ensureChat(&s, chatID)
	for i, chat := range s.Chats {
		if chat.ChatID == chatID {
			s.Chats[i].ResetDays = reset
			break
		}
	}

	return r.save(s)
}

func (r *repositoryImpl) Reset(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	now := time.Now()
	due := make(map[core.ChatID]bool)
	for i, chat := range s.Chats {
		if chat.LastReset == nil || now.Sub(*chat.LastReset) >= time.Duration(chat.ResetDays)*24*time.Hour {
			due[chat.ChatID] = true
			s.Chats[i].LastReset = &now
		}
	}

	for i, rec := range s.Reviewers {
		if due[rec.ChatID] {
			s.Reviewers[i].Weight = 0
		}
	}

	return r.save(s)
}

func (r *repositoryImpl) Clean(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-cleanAfter)
	due := make(map[core.ReviewID]bool)
	var keptReviews []reviewRecord
	for _, rec := range s.Reviews {
		if !rec.CreatedAt.After(cutoff) {
			due[rec.ID] = true
			continue
		}
		keptReviews = append(keptReviews, rec)
	}
	s.Reviews = keptReviews

	var keptMessages []reviewMessageRecord
	for _, rm := range s.ReviewMessages {
		if due[rm.ReviewID] {
			continue
		}
		keptMessages = append(keptMessages, rm)
	}
	s.ReviewMessages = keptMessages

	return r.save(s)
}

func (r *repositoryImpl) Unfreeze(_ context.Context, reviewer core.Reviewer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	avg := r.avgWeight(s, reviewer.ChatID)

	for i, rec := range s.Reviewers {
		if rec.UserID == reviewer.UserID && rec.ChatID == reviewer.ChatID {
			s.Reviewers[i].FreezeTime = nil
			s.Reviewers[i].Weight = avg
			return r.save(s)
		}
	}

	return core.ErrUserNotInReviewersList
}

func (r *repositoryImpl) Freeze(_ context.Context, reviewer core.Reviewer, date time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	for i, rec := range s.Reviewers {
		if rec.UserID == reviewer.UserID && rec.ChatID == reviewer.ChatID && !rec.IsDeleted {
			s.Reviewers[i].FreezeTime = &date
			return r.save(s)
		}
	}

	return core.ErrUserNotInReviewersList
}

func (r *repositoryImpl) ResetWeights(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, err := r.load()
	if err != nil {
		return err
	}

	today := time.Now().Format("2006-01-02")
	avgCache := make(map[core.ChatID]int)

	for i, rec := range s.Reviewers {
		if rec.IsDeleted || rec.FreezeTime == nil || rec.FreezeTime.Format("2006-01-02") != today {
			continue
		}
		avg, ok := avgCache[rec.ChatID]
		if !ok {
			avg = r.avgWeight(s, rec.ChatID)
			avgCache[rec.ChatID] = avg
		}
		s.Reviewers[i].Weight = avg
	}

	return r.save(s)
}
