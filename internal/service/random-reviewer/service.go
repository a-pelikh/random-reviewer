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
	hasher     *hasher[core.UserID]
}

func New(repository core.ReviewersRepository, secret string) (core.ReviewersService, error) {
	hash, err := newHasher[core.UserID](secret)
	if err != nil {
		return nil, fmt.Errorf("new hasher: %w", err)
	}

	return &serviceImpl{
		repository: repository,
		hasher:     hash,
	}, nil
}

func (s *serviceImpl) AddReviewer(ctx context.Context, reviewer core.Reviewer) error {
	var err error
	reviewer.ID, err = s.hasher.Encode(reviewer.ID)
	if err != nil {
		return fmt.Errorf("hash encode: %w", err)
	}
	return s.repository.AddReviewer(ctx, reviewer)
}

func (s *serviceImpl) AssignReviewer(ctx context.Context, review core.Review) error {
	if review.ReviewerID != "" {
		var err error
		review.ReviewerID, err = s.hasher.Encode(review.ReviewerID)
		if err != nil {
			return fmt.Errorf("hash encode reviewer: %w", err)
		}
	}

	return s.repository.AssignReviewer(ctx, review)
}

func (s *serviceImpl) RerollReview(ctx context.Context, chatID core.ChatID, replyMsgID core.MessageID, requesterID core.UserID) (core.UserID, core.MessageID, error) {
	rootMsgID, chainReviewers, err := s.repository.GetChainReviewers(ctx, replyMsgID)
	if err != nil {
		return "", "", fmt.Errorf("get chain reviewers: %w", err)
	}

	excluded := make(map[core.UserID]struct{}, len(chainReviewers))
	for _, id := range chainReviewers {
		excluded[id] = struct{}{}
	}

	hashedRequesterID, err := s.hasher.Encode(requesterID)
	if err != nil {
		return "", "", fmt.Errorf("hash encode requester: %w", err)
	}
	excluded[hashedRequesterID] = struct{}{}

	reviewers, err := s.repository.GetAvailableReviewers(ctx, chatID)
	if err != nil {
		return "", "", fmt.Errorf("get available reviewers: %w", err)
	}

	reviewers = slices.DeleteFunc(reviewers, func(r core.Reviewer) bool {
		_, ok := excluded[r.ID]
		return ok
	})

	if len(reviewers) == 0 {
		return "", "", core.ErrNoAnotherReviewersAllowed
	}

	reviewer := s.pickReviewer(reviewers)
	reviewer, err = s.hasher.Decode(reviewer)
	if err != nil {
		return "", "", fmt.Errorf("decode reviewer: %w", err)
	}

	return reviewer, rootMsgID, nil
}

func (s *serviceImpl) GetReviewer(ctx context.Context, chatID core.ChatID, requesterID core.UserID) (core.UserID, error) {
	reviewers, err := s.repository.GetAvailableReviewers(ctx, chatID)
	if err != nil {
		var zero core.UserID
		return zero, err
	}

	hashedRequesterID, err := s.hasher.Encode(requesterID)
	if err != nil {
		return "", fmt.Errorf("hash encode requester: %w", err)
	}

	reviewers = slices.DeleteFunc(reviewers, func(r core.Reviewer) bool {
		return r.ID == hashedRequesterID
	})

	if len(reviewers) == 0 {
		return "", core.ErrNoReviewersAvailable
	}

	reviewer := s.pickReviewer(reviewers)
	reviewer, err = s.hasher.Decode(reviewer)
	if err != nil {
		return "", fmt.Errorf("decode reviewer: %w", err)
	}

	return reviewer, nil
}

func (s *serviceImpl) RemoveReviewer(ctx context.Context, reviewer core.Reviewer) error {
	var err error
	reviewer.ID, err = s.hasher.Encode(reviewer.ID)
	if err != nil {
		return fmt.Errorf("hash encode: %w", err)
	}
	return s.repository.RemoveReviewer(ctx, reviewer)
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
