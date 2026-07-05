package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"randomreviewer/internal/core"
)

type repositoryImpl struct {
	db *sql.DB
}

func New(db *sql.DB) core.ReviewersRepository {
	return &repositoryImpl{
		db: db,
	}
}

func (r *repositoryImpl) AddReviewer(ctx context.Context, reviewer core.Reviewer) (err error) {
	const insertReviewer = `
	INSERT INTO reviewers (user_id, chat_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;
`
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("could not start transaction: %w", err)
	}
	defer func() {
		if errPanic := recover(); errPanic != nil {
			if errRollback := tx.Rollback(); err != nil {
				slog.Warn("could not rollback transaction", "error", errRollback)
			}
		} else if err != nil {
			if errRollback := tx.Rollback(); err != nil {
				slog.Warn("could not rollback transaction", "error", errRollback)
			}
		} else {
			err = tx.Commit()
		}
	}()

	if err = r.addChat(ctx, tx, reviewer.ChatID); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, insertReviewer, reviewer.ID, reviewer.ChatID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return core.ErrUserAlreadyAdded
	}

	return nil
}

func (r *repositoryImpl) RemoveReviewer(ctx context.Context, reviewer core.Reviewer) error {
	const deleteReviewer = `
	DELETE FROM reviewers WHERE user_id = $1 AND chat_id = $2;
`
	result, err := r.db.ExecContext(ctx, deleteReviewer, reviewer.ID, reviewer.ChatID)
	if err != nil {
		return fmt.Errorf("could not delete reviewer: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return core.ErrUserNotInReviewersList
	}

	return nil
}

func (r *repositoryImpl) addChat(ctx context.Context, tx *sql.Tx, chatID core.ChatID) error {
	const insertChat = `
	INSERT INTO chats (chat_id) VALUES ($1) ON CONFLICT DO NOTHING;
`
	_, err := tx.ExecContext(ctx, insertChat, chatID)
	if err != nil {
		return fmt.Errorf("could not insert chat: %w", err)
	}

	return nil
}
