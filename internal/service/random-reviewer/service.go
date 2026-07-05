package random_reviewer

import (
	"cmp"
	"context"
	"fmt"
	"math/rand"
	"slices"

	"randomreviewer/internal/core"
)

type serviceImpl struct {
	repository core.ReviewersRepository
}

func New(repository core.ReviewersRepository) core.ReviewersService {
	return &serviceImpl{
		repository: repository,
	}
}

func (s *serviceImpl) AddReviewer(ctx context.Context, reviewer core.Reviewer) error {
	err := s.repository.AddReviewer(ctx, reviewer)
	if err != nil {
		return err
	}

	return nil
}

func (s *serviceImpl) AssignReviewer(ctx context.Context, review core.Review) error {
	return s.repository.AssignReviewer(ctx, review)
}

func (s *serviceImpl) RerollLastReviewer(ctx context.Context, chatID core.ChatID, messageID core.MessageID) (core.UserID, core.UserID, error) {
	reviewers, err := s.repository.GetAvailableReviewers(ctx, chatID)
	if err != nil {
		return "", "", fmt.Errorf("get available reviewers: %w", err)
	}

	prevReviewer, err := s.repository.GetActualReviewer(ctx, messageID)
	if err != nil {
		return "", "", fmt.Errorf("get actual reviewer: %w", err)
	}

	reviewers = slices.DeleteFunc(reviewers, func(r core.Reviewer) bool {
		return r.ID == prevReviewer
	})

	if len(reviewers) == 0 {
		return "", "", core.ErrNoAnotherReviewersAllowed
	}

	return s.pickReviewer(reviewers), prevReviewer, nil
}

func (s *serviceImpl) GetReviewer(ctx context.Context, chatID core.ChatID) (core.UserID, error) {
	reviewers, err := s.repository.GetAvailableReviewers(ctx, chatID)
	if err != nil {
		var zero core.UserID
		return zero, err
	}
	if len(reviewers) == 0 {
		return "", core.ErrNoReviewersAvailable
	}

	return s.pickReviewer(reviewers), nil
}

func (s *serviceImpl) RemoveReviewer(ctx context.Context, reviewer core.Reviewer) error {
	err := s.repository.RemoveReviewer(ctx, reviewer)
	if err != nil {
		return err
	}

	return nil
}

func (s *serviceImpl) SetReset(ctx context.Context, chat core.Chat) error {
	//TODO implement me
	panic("implement me")
}

func (s *serviceImpl) Reset(ctx context.Context) error {
	//TODO: implement me
	panic("implement me")
}

func (s *serviceImpl) pickReviewer(reviewers []core.Reviewer) core.UserID {
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
			return r.ID
		}
	}

	return reviewers[len(reviewers)-1].ID
}
