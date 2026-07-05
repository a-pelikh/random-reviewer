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
	INSERT INTO reviewers (user_id, chat_id) VALUES ($1, $2)
	ON CONFLICT (user_id, chat_id) DO UPDATE SET is_deleted = FALSE
	WHERE reviewers.is_deleted = TRUE;
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
	const query = `
	UPDATE reviewers SET is_deleted = TRUE
	WHERE user_id = $1 AND chat_id = $2 AND is_deleted = FALSE;
`
	result, err := r.db.ExecContext(ctx, query, reviewer.ID, reviewer.ChatID)
	if err != nil {
		return fmt.Errorf("could not remove reviewer: %w", err)
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

func (r *repositoryImpl) GetAvailableReviewers(ctx context.Context, chatID core.ChatID) ([]core.Reviewer, error) {
	const query = `
	SELECT user_id, weight FROM reviewers
	WHERE chat_id = $1
	  AND is_deleted = FALSE
	  AND (freeze_time IS NULL OR freeze_time < NOW());
`
	rows, err := r.db.QueryContext(ctx, query, chatID)
	if err != nil {
		return nil, fmt.Errorf("could not query reviewers: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("could not close rows", "error", err)
		}
	}()

	var reviewers []core.Reviewer
	for rows.Next() {
		var rev core.Reviewer
		if err := rows.Scan(&rev.ID, &rev.Weight); err != nil {
			return nil, fmt.Errorf("could not scan reviewer: %w", err)
		}
		rev.ChatID = chatID
		reviewers = append(reviewers, rev)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return reviewers, nil
}

func (r *repositoryImpl) IncrementWeight(ctx context.Context, chatID core.ChatID, userID core.UserID) error {
	const query = `
	UPDATE reviewers SET weight = weight + 1
	WHERE chat_id = $1 AND user_id = $2;
`
	result, err := r.db.ExecContext(ctx, query, chatID, userID)
	if err != nil {
		return fmt.Errorf("could not increment weight: %w", err)
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

func (r *repositoryImpl) AssignReviewer(ctx context.Context, review core.Review) (err error) {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("could not start transaction: %w", err)
	}
	defer func() {
		if err != nil {
			if errRollback := tx.Rollback(); errRollback != nil {
				slog.Warn("could not rollback transaction", "error", errRollback)
			}
		} else {
			err = tx.Commit()
		}
	}()

	if err = r.incrementWeightTx(ctx, tx, review.ChatID, review.ReviewerID); err != nil {
		return fmt.Errorf("could not increment weight: %w", err)
	}

	if err = r.insertReviewTx(ctx, tx, review); err != nil {
		return fmt.Errorf("could not insert review: %w", err)
	}

	return nil
}

func (r *repositoryImpl) incrementWeightTx(ctx context.Context, tx *sql.Tx, chatID core.ChatID, userID core.UserID) error {
	const query = `
	UPDATE reviewers SET weight = weight + 1
	WHERE chat_id = $1 AND user_id = $2;
`
	result, err := tx.ExecContext(ctx, query, chatID, userID)
	if err != nil {
		return fmt.Errorf("could not increment weight: %w", err)
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

func (r *repositoryImpl) insertReviewTx(ctx context.Context, tx *sql.Tx, review core.Review) error {
	const query = `
	INSERT INTO reviews (reviewer_id, chat_id, message_id, prev_reviewer_id)
	VALUES ($1, $2, $3, $4);
`
	_, err := tx.ExecContext(ctx, query, review.ReviewerID, review.ChatID, review.MessageID, review.PrevReviewerID)
	if err != nil {
		return fmt.Errorf("could not insert review: %w", err)
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
