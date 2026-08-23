CREATE TABLE IF NOT EXISTS chats
(
    chat_id    VARCHAR(255) PRIMARY KEY,
    reset_days INT NOT NULL DEFAULT 14,
    CHECK ( reset_days > 0 ),
    last_reset TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS reviewers
(
    reviewer_id BIGSERIAL PRIMARY KEY,
    user_id     VARCHAR(255) NOT NULL,
    chat_id     VARCHAR(255) NOT NULL,
    UNIQUE (user_id, chat_id),
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id),
    weight      INT          NOT NULL DEFAULT 0,
    CHECK ( weight >= 0 ),
    freeze_time TIMESTAMPTZ,
    is_deleted  BOOLEAN      NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS reviews
(
    review_id   BIGSERIAL PRIMARY KEY,
    reviewer_id BIGINT       NOT NULL,
    FOREIGN KEY (reviewer_id) REFERENCES reviewers (reviewer_id),
    owner_id    VARCHAR(255) NOT NULL,
    message_id  VARCHAR(255),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS reviews_messages
(
    review_id   BIGINT       NOT NULL,
    FOREIGN KEY (review_id) REFERENCES reviews (review_id),
    reviewer_id BIGINT       NOT NULL,
    FOREIGN KEY (reviewer_id) REFERENCES reviewers (reviewer_id),
    message_id  VARCHAR(255) NOT NULL,
    PRIMARY KEY (review_id, reviewer_id, message_id)
);