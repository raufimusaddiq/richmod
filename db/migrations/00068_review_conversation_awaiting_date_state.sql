-- +goose Up
-- UIR-03: a date-only review asks for the transaction date in a bound reply. That
-- needs its own conversation state so the reply lane routes it to the date resolver
-- instead of the generic description field.
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

-- +goose Down
ALTER TABLE review_conversation DROP CONSTRAINT review_conversation_state_check;
ALTER TABLE review_conversation ADD CONSTRAINT review_conversation_state_check CHECK (
  state IN (
    'AWAITING_MERCHANT'
    ,'AWAITING_CATEGORY'
    ,'AWAITING_DETAIL'
    ,'AWAITING_PURPOSE'
    ,'AWAITING_CONFIRMATION'
    ,'AWAITING_MERCHANT_DECISION'
    ,'AWAITING_ASSET_WEALTH'
    ,'RESOLVED'
  )
);
