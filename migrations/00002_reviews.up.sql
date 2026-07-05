CREATE TABLE IF NOT EXISTS reviews
(
    review_id        BIGSERIAL PRIMARY KEY,
    reviewer_id      VARCHAR(125) NOT NULL,
    chat_id          VARCHAR(125) NOT NULL,
    FOREIGN KEY (reviewer_id, chat_id) REFERENCES reviewers (user_id, chat_id),
    message_id       VARCHAR(125) NOT NULL,
    prev_reviewer_id VARCHAR(125),
    FOREIGN KEY (prev_reviewer_id, chat_id) REFERENCES reviewers (user_id, chat_id)
);