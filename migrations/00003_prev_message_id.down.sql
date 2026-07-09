ALTER TABLE reviews DROP COLUMN IF EXISTS root_message_id;
ALTER TABLE reviews DROP COLUMN IF EXISTS prev_message_id;
ALTER TABLE reviews ALTER COLUMN reviewer_id SET NOT NULL;
ALTER TABLE reviews ADD CONSTRAINT reviews_reviewer_id_chat_id_fkey FOREIGN KEY (reviewer_id, chat_id) REFERENCES reviewers (user_id, chat_id);
ALTER TABLE reviews DROP CONSTRAINT IF EXISTS reviews_message_id_unique;
ALTER TABLE reviews ADD COLUMN prev_reviewer_id VARCHAR(125);
ALTER TABLE reviews ADD CONSTRAINT reviews_prev_reviewer_id_chat_id_fkey FOREIGN KEY (prev_reviewer_id, chat_id) REFERENCES reviewers (user_id, chat_id);