-- +goose Up
-- UIR-08: the optional merchant-learning question must not keep a review_item
-- open. The review completes at confirm time; the pending question is tracked by
-- a distinct conversation state so review_request.status can be RESOLVED safely.
ALTER TABLE review_conversation DROP CONSTRAINT review_conversation_state_check;
ALTER TABLE review_conversation ADD CONSTRAINT review_conversation_state_check CHECK (
  state IN ('AWAITING_MERCHANT','AWAITING_CATEGORY','AWAITING_DETAIL','AWAITING_PURPOSE','AWAITING_CONFIRMATION','AWAITING_MERCHANT_DECISION','RESOLVED')
);

-- +goose Down
ALTER TABLE review_conversation DROP CONSTRAINT review_conversation_state_check;
ALTER TABLE review_conversation ADD CONSTRAINT review_conversation_state_check CHECK (
  state IN ('AWAITING_MERCHANT','AWAITING_CATEGORY','AWAITING_DETAIL','AWAITING_PURPOSE','AWAITING_CONFIRMATION','RESOLVED')
);
