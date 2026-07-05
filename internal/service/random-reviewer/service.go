package random_reviewer

import (
	"context"

	"randomreviewer/internal/core"
)

type serviceImpl struct {
}

func New() core.ReviewersService {
	return &serviceImpl{}
}

func (s *serviceImpl) UpdateActualChatMembers(ctx context.Context, chatID string, chatMembers []core.ChatMember) error {
	//TODO implement me
	panic("implement me")
}

func (s *serviceImpl) RerollLastReviewer(ctx context.Context, chatID string, chatMember core.ChatMember) (core.ChatMember, error) {
	//TODO implement me
	panic("implement me")
}

func (s *serviceImpl) GetReviewer(ctx context.Context, chatID string) (core.ChatMember, error) {
	//TODO implement me
	panic("implement me")
}
