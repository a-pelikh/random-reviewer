package postgres

import "randomreviewer/internal/core"

type repositoryImpl struct {
}

func New() core.ReviewersRepository {
	return &repositoryImpl{}
}
