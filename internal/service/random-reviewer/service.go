package random_reviewer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"slices"
	"time"

	"randomreviewer/internal/core"
)

type serviceImpl struct {
	repository core.ReviewersRepository
}

func New(repository core.ReviewersRepository) core.ReviewersService {
	return &serviceImpl{repository: repository}
}

func (s *serviceImpl) GetReviewers(ctx context.Context, chatID core.ChatID) ([]core.Reviewer, error) {
	return s.repository.GetReviewers(ctx, chatID)
}

func (s *serviceImpl) AddReviewer(ctx context.Context, reviewer core.Reviewer) error {
	if err := s.repository.AddReviewer(ctx, reviewer); err != nil {
		return fmt.Errorf("add reviewer: %w", err)
	}

	return nil
}

func (s *serviceImpl) AssignReviewer(ctx context.Context, chatID core.ChatID, ownerID core.UserID, repliedMessageIDs ...core.MessageID) (core.UserID, core.ReviewID, error) {
	var (
		review core.Review
		err    error
	)
	if len(repliedMessageIDs) > 0 {
		review, err = s.repository.GetReview(ctx, repliedMessageIDs[0])
		if err != nil && !errors.Is(err, core.ErrReviewNotFound) {
			return "", 0, fmt.Errorf("get review: %w", err)
		}
	}

	if len(repliedMessageIDs) == 0 || errors.Is(err, core.ErrReviewNotFound) {
		reviewers, err := s.repository.GetReviewers(ctx, chatID)
		if err != nil {
			return "", 0, fmt.Errorf("get chat reviewers: %w", err)
		}

		reviewers = slices.DeleteFunc(reviewers, func(reviewer core.Reviewer) bool {
			return reviewer.UserID == ownerID || reviewer.FreezeTime.After(time.Now())
		})
		if len(reviewers) == 0 {
			return "", 0, core.ErrNoReviewersAvailable
		}

		reviewer := s.pickReviewer(reviewers)
		reviewID, err := s.repository.AssignReviewer(ctx, core.Review{
			ReviewerID: reviewer.ID,
			OwnerID:    ownerID,
		})
		if err != nil {
			return "", 0, fmt.Errorf("assign reviewer: %w", err)
		}

		err = s.repository.SaveReviewMessages(ctx, reviewID, reviewer.ID, repliedMessageIDs...)
		if err != nil {
			return "", 0, fmt.Errorf("save review messages: %w", err)
		}
		return reviewer.UserID, reviewID, nil
	}

	reviewers, err := s.repository.GetAvailableReviewers(ctx, review.ID)
	if err != nil {
		return "", 0, fmt.Errorf("get available reviewers: %w", err)
	}

	if len(reviewers) == 0 {
		return "", 0, core.ErrNoAnotherReviewersAllowed
	}

	reviewer := s.pickReviewer(reviewers)

	err = s.repository.RerollReviewer(ctx, reviewer.ID, review.ID)
	if err != nil {
		return "", 0, fmt.Errorf("reroll reviewer: %w", err)
	}

	return reviewer.UserID, review.ID, nil
}

func (s *serviceImpl) SetMessageID(ctx context.Context, reviewID core.ReviewID, messageID core.MessageID) (core.MessageID, error) {
	return s.repository.SetMessageID(ctx, reviewID, messageID)
}

func (s *serviceImpl) RemoveReviewer(ctx context.Context, reviewer core.Reviewer) error {
	return s.repository.RemoveReviewer(ctx, reviewer)
}

func (s *serviceImpl) SetReset(ctx context.Context, chatID core.ChatID, reset int) error {
	return s.repository.SetReset(ctx, chatID, reset)
}

func (s *serviceImpl) GetStats(ctx context.Context, chatID core.ChatID) (core.ChatStats, error) {
	return s.repository.GetStats(ctx, chatID)
}

func (s *serviceImpl) Reset(ctx context.Context) {
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Error("context error", slog.String("error", ctx.Err().Error()))
			return
		case <-ticker.C:
			err := s.repository.Reset(ctx)
			if err != nil {
				slog.Error("reset error", slog.String("error", err.Error()))
			}
		}
	}
}

func (s *serviceImpl) Clean(ctx context.Context) {
	ticker := time.NewTicker(time.Hour * 24)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Error("context error", slog.String("error", ctx.Err().Error()))
			return
		case <-ticker.C:
			if err := s.repository.Clean(ctx); err != nil {
				slog.Error("clean error", slog.String("error", err.Error()))
			}
		}
	}
}

func (s *serviceImpl) Freeze(ctx context.Context, reviewer core.Reviewer, date time.Time) error {
	return s.repository.Freeze(ctx, reviewer, date)
}

func (s *serviceImpl) Unfreeze(ctx context.Context, reviewer core.Reviewer) error {
	return s.repository.Unfreeze(ctx, reviewer)
}

func (s *serviceImpl) ResetWeights(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Error("context error", slog.String("error", ctx.Err().Error()))
			return
		case <-ticker.C:
			if err := s.repository.ResetWeights(ctx); err != nil {
				slog.Error("reset weights error", slog.String("error", err.Error()))
			}
		}
	}
}

func (s *serviceImpl) pickReviewer(reviewers []core.Reviewer) core.Reviewer {
	maxWeight := slices.MaxFunc(reviewers, func(a, b core.Reviewer) int {
		return cmp.Compare(a.Weight, b.Weight)
	}).Weight

	var total int
	for _, r := range reviewers {
		total += maxWeight - r.Weight + 1
	}

	pick := rand.Intn(total)
	var cumulative int
	for _, r := range reviewers {
		cumulative += maxWeight - r.Weight + 1
		if pick < cumulative {
			return r
		}
	}

	return reviewers[len(reviewers)-1]
}
