-- +goose Up
ALTER TABLE review_conversation DROP CONSTRAINT review_conversation_state_check;
ALTER TABLE review_conversation ADD CONSTRAINT review_conversation_state_check CHECK (
  state IN (
    'AWAITING_MERCHANT'
    ,'AWAITING_CATEGORY'
    ,'AWAITING_DETAIL'
    ,'AWAITING_DATE'
    ,'AWAITING_PURPOSE'
    ,'AWAITING_CONFIRMATION'
    ,'AWAITING_MERCHANT_DECISION'
    ,'AWAITING_ASSET_WEALTH'
    ,'AWAITING_INVESTMENT_ACCOUNT'
    ,'RESOLVED'
  )
);

-- +goose Down
UPDATE review_conversation SET state='AWAITING_DETAIL' WHERE state='AWAITING_INVESTMENT_ACCOUNT';
ALTER TABLE review_conversation DROP CONSTRAINT review_conversation_state_check;
ALTER TABLE review_conversation ADD CONSTRAINT review_conversation_state_check CHECK (
  state IN (
    'AWAITING_MERCHANT'
    ,'AWAITING_CATEGORY'
    ,'AWAITING_DETAIL'
    ,'AWAITING_DATE'
    ,'AWAITING_PURPOSE'
    ,'AWAITING_CONFIRMATION'
    ,'AWAITING_MERCHANT_DECISION'
    ,'AWAITING_ASSET_WEALTH'
    ,'RESOLVED'
  )
);
