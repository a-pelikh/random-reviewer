package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"randomreviewer/internal/core"
)

type repositoryImpl struct {
	db *sql.DB
}

func New(db *sql.DB) core.ReviewersRepository {
	return &repositoryImpl{db: db}
}

func (r *repositoryImpl) GetReview(ctx context.Context, messageID core.MessageID) (core.Review, error) {
	const query = `
		SELECT 
			r.review_id,
			r.reviewer_id,
			r.owner_id,
			r.message_id,
			r.created_at
		FROM reviews r
		JOIN reviews_messages rm on r.review_id = rm.review_id
		WHERE rm.message_id = $1
		LIMIT 1
	`

	var review core.Review
	if err := r.db.QueryRowContext(ctx, query, messageID).Scan(
		&review.ID,
		&review.ReviewerID,
		&review.OwnerID,
		&review.MessageID,
		&review.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return review, core.ErrReviewNotFound
		}

		return review, fmt.Errorf("query row: %w", err)
	}

	return review, nil
}

func (r *repositoryImpl) AssignReviewer(ctx context.Context, review core.Review) (reviewID core.ReviewID, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				slog.Error("rollback tx err", "error", rollbackErr)
			}
			panic(p)
		} else if err != nil {
			slog.Error("assign reviewer err", "error", err)
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				slog.Error("rollback tx err", "error", rollbackErr)
			}
		} else {
			err = tx.Commit()
		}
	}()

	const query = `
		INSERT INTO reviews (reviewer_id, owner_id) VALUES ($1, $2) RETURNING review_id
	`

	if err = tx.QueryRowContext(ctx, query, review.ReviewerID, review.OwnerID).Scan(&reviewID); err != nil {
		return 0, fmt.Errorf("insert review: %w", err)
	}

	if err = r.incrementWeight(ctx, tx, review.ReviewerID); err != nil {
		return 0, err
	}

	return reviewID, nil
}

func (r *repositoryImpl) incrementWeight(ctx context.Context, tx *sql.Tx, reviewerID core.ReviewerID) error {
	const query = `UPDATE reviewers SET weight = weight + 1 WHERE reviewer_id = $1`
	if _, err := tx.ExecContext(ctx, query, reviewerID); err != nil {
		return fmt.Errorf("increment reviewer weight: %w", err)
	}
	return nil
}

func (r *repositoryImpl) decrementWeight(ctx context.Context, tx *sql.Tx, reviewerID core.ReviewerID) error {
	const query = `UPDATE reviewers SET weight = GREATEST(weight - 1, 0) WHERE reviewer_id = $1`
	if _, err := tx.ExecContext(ctx, query, reviewerID); err != nil {
		return fmt.Errorf("decrement reviewer weight: %w", err)
	}
	return nil
}

func (r *repositoryImpl) SaveReviewMessages(ctx context.Context, reviewID core.ReviewID, reviewerID core.ReviewerID, messagesID ...core.MessageID) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				slog.Error("rollback tx err", "error", rollbackErr)
			}
			panic(p)
		} else if err != nil {
			slog.Error("save review messages err", "error", err)
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				slog.Error("rollback tx err", "error", rollbackErr)
			}
		} else {
			err = tx.Commit()
		}
	}()

	const saveReviewMessagesQuery = `
		INSERT INTO reviews_messages (review_id, reviewer_id, message_id) VALUES ($1, $2, $3)
	`
	for _, messageID := range messagesID {
		if _, err = tx.ExecContext(ctx, saveReviewMessagesQuery, reviewID, reviewerID, messageID); err != nil {
			return fmt.Errorf("save review messages: %w", err)
		}
	}

	return nil
}

func (r *repositoryImpl) RerollReviewer(ctx context.Context, newReviewerID core.ReviewerID, reviewID core.ReviewID) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				slog.Error("rollback tx err", "error", rollbackErr)
			}
			panic(p)
		} else if err != nil {
			slog.Error("reroll reviewer err", "error", err)
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				slog.Error("rollback tx err", "error", rollbackErr)
			}
		} else {
			err = tx.Commit()
		}
	}()

	const getCurrentReviewerQuery = `SELECT reviewer_id FROM reviews WHERE review_id = $1`
	var oldReviewerID core.ReviewerID
	if err = tx.QueryRowContext(ctx, getCurrentReviewerQuery, reviewID).Scan(&oldReviewerID); err != nil {
		return fmt.Errorf("get current reviewer: %w", err)
	}

	const setReviewerQuery = `UPDATE reviews SET reviewer_id = $1 WHERE review_id = $2`
	if _, err = tx.ExecContext(ctx, setReviewerQuery, newReviewerID, reviewID); err != nil {
		return fmt.Errorf("set new reviewer: %w", err)
	}

	if err = r.incrementWeight(ctx, tx, newReviewerID); err != nil {
		return err
	}

	if err = r.decrementWeight(ctx, tx, oldReviewerID); err != nil {
		return err
	}

	return nil
}

func (r *repositoryImpl) GetAvailableReviewers(ctx context.Context, review core.ReviewID) ([]core.Reviewer, error) {
	const query = `
		SELECT r.reviewer_id, r.user_id, r.weight
		FROM reviewers r
		JOIN reviews rev ON rev.review_id = $1
		JOIN reviewers cur ON cur.reviewer_id = rev.reviewer_id
		WHERE r.chat_id = cur.chat_id
  			AND r.is_deleted = FALSE
  			AND (r.freeze_time IS NULL OR r.freeze_time < NOW())
  			AND r.user_id != rev.owner_id
  			AND r.reviewer_id NOT IN (
      			SELECT reviewer_id FROM reviews_messages WHERE review_id = $1
  			)
	`
	rows, err := r.db.QueryContext(ctx, query, review)
	if err != nil {
		return nil, fmt.Errorf("query available reviewers: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("close rows", "error", err)
		}
	}()

	var reviewers []core.Reviewer
	for rows.Next() {
		var rev core.Reviewer
		if err := rows.Scan(&rev.ID, &rev.UserID, &rev.Weight); err != nil {
			return nil, fmt.Errorf("scan reviewer: %w", err)
		}
		reviewers = append(reviewers, rev)
	}
	return reviewers, rows.Err()
}

func (r *repositoryImpl) SetReset(ctx context.Context, chatID core.ChatID, reset int) error {
	const query = `UPDATE chats SET reset_days = $2 WHERE chat_id = $1;`
	_, err := r.db.ExecContext(ctx, query, chatID, reset)
	if err != nil {
		return fmt.Errorf("set reset: %w", err)
	}
	return nil
}

func (r *repositoryImpl) Reset(ctx context.Context) error {
	const query = `
		WITH due_chats AS (
			SELECT chat_id FROM chats
			WHERE last_reset IS NULL OR last_reset <= NOW() - make_interval(days => reset_days)
		),
		reset_weights AS (
			UPDATE reviewers SET weight = 0
			WHERE chat_id IN (SELECT chat_id FROM due_chats)
		)
		UPDATE chats SET last_reset = NOW()
		WHERE chat_id IN (SELECT chat_id FROM due_chats);
	`
	if _, err := r.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	return nil
}

func (r *repositoryImpl) Clean(ctx context.Context) error {
	const query = `
		WITH due_reviews AS (
			SELECT review_id FROM reviews
			WHERE created_at <= NOW() - INTERVAL '1 months'
		),
		del_messages AS (
			DELETE FROM reviews_messages
			WHERE review_id IN (SELECT review_id FROM due_reviews)
		)
		DELETE FROM reviews
		WHERE review_id IN (SELECT review_id FROM due_reviews);
	`
	if _, err := r.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("clean: %w", err)
	}
	return nil
}

func (r *repositoryImpl) Unfreeze(ctx context.Context, reviewer core.Reviewer) error {
	const query = `
		WITH avg_weight AS (
			SELECT COALESCE(CEIL(AVG(weight))::int, 0) AS w
			FROM reviewers
			WHERE chat_id = $2 AND is_deleted = FALSE
			  AND (freeze_time IS NULL OR freeze_time < NOW())
		)
		UPDATE reviewers SET freeze_time = NULL, weight = avg_weight.w
		FROM avg_weight
		WHERE user_id = $1 AND chat_id = $2;
	`
	result, err := r.db.ExecContext(ctx, query, reviewer.UserID, reviewer.ChatID)
	if err != nil {
		return fmt.Errorf("unfreeze reviewer: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return core.ErrUserNotInReviewersList
	}

	return nil
}

func (r *repositoryImpl) Freeze(ctx context.Context, reviewer core.Reviewer, date time.Time) error {
	const query = `
		UPDATE reviewers SET freeze_time = $3
		WHERE user_id = $1 AND chat_id = $2 AND is_deleted = FALSE;
	`
	result, err := r.db.ExecContext(ctx, query, reviewer.UserID, reviewer.ChatID, date)
	if err != nil {
		return fmt.Errorf("freeze reviewer: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return core.ErrUserNotInReviewersList
	}

	return nil
}

func (r *repositoryImpl) ResetWeights(ctx context.Context) error {
	const query = `
		WITH avg_per_chat AS (
			SELECT chat_id, COALESCE(CEIL(AVG(weight))::int, 0) AS w
			FROM reviewers
			WHERE is_deleted = FALSE AND (freeze_time IS NULL OR freeze_time < NOW())
			GROUP BY chat_id
		)
		UPDATE reviewers r
		SET weight = a.w
		FROM avg_per_chat a
		WHERE r.chat_id = a.chat_id
		  AND r.is_deleted = FALSE
		  AND r.freeze_time::date = NOW()::date;
	`
	if _, err := r.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("reset weights: %w", err)
	}
	return nil
}

func (r *repositoryImpl) GetReviewers(ctx context.Context, chatID core.ChatID) ([]core.Reviewer, error) {
	const query = `
		SELECT r.reviewer_id, r.user_id, r.weight, r.freeze_time,
		       u.first_name, u.last_name
		FROM reviewers r
		LEFT JOIN users u ON u.user_id = r.user_id
		WHERE r.chat_id = $1 AND r.is_deleted = FALSE
	`
	rows, err := r.db.QueryContext(ctx, query, chatID)
	if err != nil {
		return nil, fmt.Errorf("query reviewers: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Warn("close rows", "error", err)
		}
	}()

	var reviewers []core.Reviewer
	for rows.Next() {
		var rev core.Reviewer
		var freezeTime sql.NullTime
		var firstName, lastName sql.NullString
		if err := rows.Scan(&rev.ID, &rev.UserID, &rev.Weight, &freezeTime, &firstName, &lastName); err != nil {
			return nil, fmt.Errorf("scan reviewer: %w", err)
		}
		rev.ChatID = chatID
		rev.FreezeTime = freezeTime.Time
		rev.FirstName = firstName.String
		rev.LastName = lastName.String
		reviewers = append(reviewers, rev)
	}
	return reviewers, rows.Err()
}

func (r *repositoryImpl) SaveUser(ctx context.Context, userID core.UserID, firstName, lastName string) error {
	const query = `
		INSERT INTO users (user_id, first_name, last_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE
			SET first_name = EXCLUDED.first_name,
			    last_name = EXCLUDED.last_name
	`
	if _, err := r.db.ExecContext(ctx, query, userID, firstName, lastName); err != nil {
		return fmt.Errorf("save user: %w", err)
	}
	return nil
}

func (r *repositoryImpl) GetStats(ctx context.Context, chatID core.ChatID) (core.ChatStats, error) {
	const sinceQuery = `
		SELECT MIN(rev.created_at)
		FROM reviews rev
		JOIN reviewers r ON r.reviewer_id = rev.reviewer_id
		WHERE r.chat_id = $1
	`
	var stats core.ChatStats
	var since sql.NullTime
	if err := r.db.QueryRowContext(ctx, sinceQuery, chatID).Scan(&since); err != nil {
		return core.ChatStats{}, fmt.Errorf("query since: %w", err)
	}
	if since.Valid {
		stats.Since = since.Time
	}

	const authorsQuery = `
		SELECT rev.owner_id, u.first_name, u.last_name, COUNT(*) AS mrs
		FROM reviews rev
		JOIN reviewers r ON r.reviewer_id = rev.reviewer_id
		LEFT JOIN users u ON u.user_id = rev.owner_id
		WHERE r.chat_id = $1
		GROUP BY rev.owner_id, u.first_name, u.last_name
		ORDER BY mrs DESC
	`
	authorRows, err := r.db.QueryContext(ctx, authorsQuery, chatID)
	if err != nil {
		return core.ChatStats{}, fmt.Errorf("query authors: %w", err)
	}
	defer func() {
		if err := authorRows.Close(); err != nil {
			slog.Warn("close rows", "error", err)
		}
	}()

	var authors []core.AuthorStats
	for authorRows.Next() {
		var a core.AuthorStats
		var firstName, lastName sql.NullString
		if err := authorRows.Scan(&a.UserID, &firstName, &lastName, &a.MRs); err != nil {
			return core.ChatStats{}, fmt.Errorf("scan author: %w", err)
		}
		a.FirstName = firstName.String
		a.LastName = lastName.String
		authors = append(authors, a)
	}
	if err := authorRows.Err(); err != nil {
		return core.ChatStats{}, fmt.Errorf("rows err: %w", err)
	}

	const reviewersQuery = `
		SELECT r.user_id, u.first_name, u.last_name, COUNT(rev.review_id) AS reviews
		FROM reviewers r
		JOIN reviews rev ON rev.reviewer_id = r.reviewer_id
		LEFT JOIN users u ON u.user_id = r.user_id
		WHERE r.chat_id = $1
		GROUP BY r.user_id, u.first_name, u.last_name
		ORDER BY reviews DESC
	`
	reviewerRows, err := r.db.QueryContext(ctx, reviewersQuery, chatID)
	if err != nil {
		return core.ChatStats{}, fmt.Errorf("query reviewers: %w", err)
	}
	defer func() {
		if err := reviewerRows.Close(); err != nil {
			slog.Warn("close rows", "error", err)
		}
	}()

	var reviewers []core.ReviewerStats
	for reviewerRows.Next() {
		var rs core.ReviewerStats
		var firstName, lastName sql.NullString
		if err := reviewerRows.Scan(&rs.UserID, &firstName, &lastName, &rs.Reviews); err != nil {
			return core.ChatStats{}, fmt.Errorf("scan reviewer: %w", err)
		}
		rs.FirstName = firstName.String
		rs.LastName = lastName.String
		reviewers = append(reviewers, rs)
	}
	if err := reviewerRows.Err(); err != nil {
		return core.ChatStats{}, fmt.Errorf("rows err: %w", err)
	}

	stats.Authors = authors
	stats.Reviewers = reviewers
	return stats, nil
}

func (r *repositoryImpl) AddReviewer(ctx context.Context, reviewer core.Reviewer) (err error) {
	const query = `
		WITH avg_weight AS (
			SELECT COALESCE(CEIL(AVG(weight))::int, 0) AS w
			FROM reviewers
			WHERE chat_id = $2 AND is_deleted = FALSE
			  AND (freeze_time IS NULL OR freeze_time < NOW())
		)
		INSERT INTO reviewers (user_id, chat_id, weight)
		SELECT $1, $2, w FROM avg_weight
		ON CONFLICT (user_id, chat_id) DO UPDATE
			SET is_deleted = FALSE, weight = EXCLUDED.weight
			WHERE reviewers.is_deleted = TRUE
			  AND (reviewers.freeze_time IS NULL OR reviewers.freeze_time < NOW());
	`
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				slog.Warn("rollback", "error", rbErr)
			}
		} else {
			err = tx.Commit()
		}
	}()

	if err = r.addChat(ctx, tx, reviewer.ChatID); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, query, reviewer.UserID, reviewer.ChatID)
	if err != nil {
		return fmt.Errorf("insert reviewer: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return core.ErrUserAlreadyAdded
	}

	return nil
}

func (r *repositoryImpl) RemoveReviewer(ctx context.Context, reviewer core.Reviewer) error {
	const query = `
		UPDATE reviewers SET is_deleted = TRUE
		WHERE user_id = $1 AND chat_id = $2 AND is_deleted = FALSE;
	`
	result, err := r.db.ExecContext(ctx, query, reviewer.UserID, reviewer.ChatID)
	if err != nil {
		return fmt.Errorf("remove reviewer: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return core.ErrUserNotInReviewersList
	}
	return nil
}

func (r *repositoryImpl) SetMessageID(ctx context.Context, reviewID core.ReviewID, messageID core.MessageID) (oldMessageID core.MessageID, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				slog.Error("rollback tx err", "error", rollbackErr)
			}
			panic(p)
		} else if err != nil {
			slog.Error("set message id err", "error", err)
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				slog.Error("rollback tx err", "error", rollbackErr)
			}
		} else {
			err = tx.Commit()
		}
	}()

	const updateQuery = `
		WITH old AS (
			SELECT message_id, reviewer_id FROM reviews WHERE review_id = $1 FOR UPDATE
		)
		UPDATE reviews SET message_id = $2
		FROM old
		WHERE reviews.review_id = $1
		RETURNING old.message_id, old.reviewer_id;
	`
	var oldMsg sql.NullString
	var reviewerID core.ReviewerID
	if err = tx.QueryRowContext(ctx, updateQuery, reviewID, messageID).Scan(&oldMsg, &reviewerID); err != nil {
		return "", fmt.Errorf("set message id: %w", err)
	}
	if oldMsg.Valid {
		oldMessageID = core.MessageID(oldMsg.String)
	}

	const insertQuery = `INSERT INTO reviews_messages (review_id, reviewer_id, message_id) VALUES ($1, $2, $3);`
	if _, err = tx.ExecContext(ctx, insertQuery, reviewID, reviewerID, messageID); err != nil {
		return "", fmt.Errorf("save review message: %w", err)
	}

	return oldMessageID, nil
}

func (r *repositoryImpl) addChat(ctx context.Context, tx *sql.Tx, chatID core.ChatID) error {
	const query = `INSERT INTO chats (chat_id, last_reset) VALUES ($1, NOW()) ON CONFLICT DO NOTHING;`
	_, err := tx.ExecContext(ctx, query, chatID)
	if err != nil {
		return fmt.Errorf("insert chat: %w", err)
	}
	return nil
}
