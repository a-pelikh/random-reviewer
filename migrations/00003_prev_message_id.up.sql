ALTER TABLE reviews ADD CONSTRAINT reviews_message_id_unique UNIQUE (message_id);
ALTER TABLE reviews DROP CONSTRAINT IF EXISTS reviews_prev_reviewer_id_chat_id_fkey;
ALTER TABLE reviews DROP COLUMN IF EXISTS prev_reviewer_id;

-- Make reviewer_id nullable for anchor records (M0/M1 without assigned reviewer)
ALTER TABLE reviews DROP CONSTRAINT IF EXISTS reviews_reviewer_id_chat_id_fkey;
ALTER TABLE reviews ALTER COLUMN reviewer_id DROP NOT NULL;

-- prev_message_id without FK (may reference messages outside the reviews table)
ALTER TABLE reviews ADD COLUMN prev_message_id VARCHAR(125);

-- root_message_id links all messages in a review chain to a single root
ALTER TABLE reviews ADD COLUMN root_message_id VARCHAR(125);