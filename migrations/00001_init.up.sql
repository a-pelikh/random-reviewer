CREATE TYPE reset_types AS ENUM (
    'day',
    'week',
    'month'
    );

CREATE TABLE IF NOT EXISTS chats
(
    chat_id VARCHAR(125) PRIMARY KEY,
    reset   reset_types NOT NULL DEFAULT 'week'
);

CREATE TABLE IF NOT EXISTS reviewers
(
    user_id     VARCHAR(125) NOT NULL,
    chat_id     VARCHAR(125) NOT NULL,
    PRIMARY KEY (user_id, chat_id),
    FOREIGN KEY (chat_id) REFERENCES chats (chat_id),
    weight      INTEGER      NOT NULL DEFAULT 0,
    freeze_time TIMESTAMPTZ,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE
);