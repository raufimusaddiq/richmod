-- +goose Up
-- Multi-household tenancy hardening (ADR-058).

-- Job ownership. A job's household is derived once, when it is enqueued, from
-- the payload's anchor row; the payload stays the dispatch contract. Platform
-- jobs with no household anchor keep NULL.
ALTER TABLE job ADD COLUMN household_id UUID REFERENCES household(id);

-- +goose StatementBegin
CREATE FUNCTION job_payload_uuid(p_payload JSONB, p_key TEXT) RETURNS UUID LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE WHEN p_payload->>p_key ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                THEN (p_payload->>p_key)::uuid END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION job_household(p_payload JSONB) RETURNS UUID LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT h.id FROM household h WHERE h.id = job_payload_uuid(p_payload, 'household_id')),
        (SELECT s.household_id FROM source_event s WHERE s.id = job_payload_uuid(p_payload, 'source_event_id')),
        (SELECT d.household_id FROM document d WHERE d.id = job_payload_uuid(p_payload, 'document_id')),
        (SELECT d.household_id FROM document d WHERE d.id = job_payload_uuid(p_payload, 'bind_document_id')),
        (SELECT r.household_id FROM review_request r WHERE r.id = job_payload_uuid(p_payload, 'review_request_id')),
        (SELECT r.household_id FROM review_request r WHERE r.id = job_payload_uuid(p_payload, 'bind_merchant_learning_request_id')),
        (SELECT ri.household_id FROM review_item ri WHERE ri.id = job_payload_uuid(p_payload, 'review_id')),
        (SELECT p.household_id FROM financial_email_preview p WHERE p.id = job_payload_uuid(p_payload, 'preview_id')),
        (SELECT i.household_id FROM insight i WHERE i.id = job_payload_uuid(p_payload, 'insight_id')),
        -- Telegram sends go to private chats only, whose ID is the user's ID.
        (SELECT ti.household_id FROM telegram_identity ti
          WHERE p_payload->>'chat_id' ~ '^[0-9]{1,18}$' AND ti.telegram_user_id = (p_payload->>'chat_id')::bigint)
    )
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION assign_job_household() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.household_id IS NULL THEN
        NEW.household_id := job_household(NEW.payload_json);
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd

CREATE TRIGGER job_assign_household BEFORE INSERT ON job
    FOR EACH ROW EXECUTE FUNCTION assign_job_household();

UPDATE job SET household_id = job_household(payload_json) WHERE household_id IS NULL;

-- Household-scoped operational status and fair claiming read these.
CREATE INDEX job_household_status_idx ON job (household_id, status) WHERE household_id IS NOT NULL;
CREATE INDEX job_lane_household_started_idx ON job (lane, household_id, started_at DESC) WHERE started_at IS NOT NULL;

-- Parent keys for household-composite foreign keys. A child row can only
-- reference a parent row of the same household once these exist. document
-- already has (id, household_id) unique from migration 00077 and reuses it.
CREATE UNIQUE INDEX account_household_id_id_key ON account (household_id, id);
CREATE UNIQUE INDEX attachment_household_id_id_key ON attachment (household_id, id);
CREATE UNIQUE INDEX bank_email_listener_household_id_id_key ON bank_email_listener (household_id, id);
CREATE UNIQUE INDEX category_household_id_id_key ON category (household_id, id);
CREATE UNIQUE INDEX cycle_residual_case_household_id_id_key ON cycle_residual_case (household_id, id);
CREATE UNIQUE INDEX email_ingress_address_household_id_id_key ON email_ingress_address (household_id, id);
CREATE UNIQUE INDEX email_ingress_delivery_household_id_id_key ON email_ingress_delivery (household_id, id);
CREATE UNIQUE INDEX financial_email_observation_household_id_id_key ON financial_email_observation (household_id, id);
CREATE UNIQUE INDEX financial_email_source_household_id_id_key ON financial_email_source (household_id, id);
CREATE UNIQUE INDEX merchant_household_id_id_key ON merchant (household_id, id);
CREATE UNIQUE INDEX review_item_household_id_id_key ON review_item (household_id, id);
CREATE UNIQUE INDEX salary_event_household_id_id_key ON salary_event (household_id, id);
CREATE UNIQUE INDEX salary_source_household_id_id_key ON salary_source (household_id, id);
CREATE UNIQUE INDEX source_event_household_id_id_key ON source_event (household_id, id);
CREATE UNIQUE INDEX telegram_conversation_turn_household_id_id_key ON telegram_conversation_turn (household_id, id);
CREATE UNIQUE INDEX transaction_household_id_id_key ON transaction (household_id, id);
CREATE UNIQUE INDEX transaction_proposal_household_id_id_key ON transaction_proposal (household_id, id);
CREATE UNIQUE INDEX wealth_account_household_id_id_key ON wealth_account (household_id, id);
CREATE UNIQUE INDEX wealth_observation_household_id_id_key ON wealth_observation (household_id, id);

-- Same-household references. NOT VALID + VALIDATE checks every existing row;
-- a cross-household reference already in the data fails this migration
-- explicitly instead of being repaired silently.
ALTER TABLE category ADD CONSTRAINT category_parent_id_hh_fkey FOREIGN KEY (household_id, parent_id) REFERENCES category (household_id, id) NOT VALID;
ALTER TABLE merchant_alias ADD CONSTRAINT merchant_alias_default_category_id_hh_fkey FOREIGN KEY (household_id, default_category_id) REFERENCES category (household_id, id) NOT VALID;
ALTER TABLE merchant_alias ADD CONSTRAINT merchant_alias_normalized_merchant_id_hh_fkey FOREIGN KEY (household_id, normalized_merchant_id) REFERENCES merchant (household_id, id) NOT VALID;
ALTER TABLE transaction ADD CONSTRAINT transaction_account_id_hh_fkey FOREIGN KEY (household_id, account_id) REFERENCES account (household_id, id) NOT VALID;
ALTER TABLE transaction ADD CONSTRAINT transaction_category_id_hh_fkey FOREIGN KEY (household_id, category_id) REFERENCES category (household_id, id) NOT VALID;
ALTER TABLE transaction ADD CONSTRAINT transaction_merchant_id_hh_fkey FOREIGN KEY (household_id, merchant_id) REFERENCES merchant (household_id, id) NOT VALID;
ALTER TABLE transaction ADD CONSTRAINT transaction_related_wealth_account_id_hh_fkey FOREIGN KEY (household_id, related_wealth_account_id) REFERENCES wealth_account (household_id, id) NOT VALID;
ALTER TABLE transaction_proposal ADD CONSTRAINT transaction_proposal_category_candidate_id_hh_fkey FOREIGN KEY (household_id, category_candidate_id) REFERENCES category (household_id, id) NOT VALID;
ALTER TABLE transaction_proposal ADD CONSTRAINT transaction_proposal_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE reconciliation_merge ADD CONSTRAINT reconciliation_merge_source_transaction_id_hh_fkey FOREIGN KEY (household_id, source_transaction_id) REFERENCES transaction (household_id, id) NOT VALID;
ALTER TABLE reconciliation_merge ADD CONSTRAINT reconciliation_merge_target_transaction_id_hh_fkey FOREIGN KEY (household_id, target_transaction_id) REFERENCES transaction (household_id, id) NOT VALID;
ALTER TABLE review_request ADD CONSTRAINT review_request_review_item_id_hh_fkey FOREIGN KEY (household_id, review_item_id) REFERENCES review_item (household_id, id) NOT VALID;
ALTER TABLE review_request ADD CONSTRAINT review_request_transaction_id_hh_fkey FOREIGN KEY (household_id, transaction_id) REFERENCES transaction (household_id, id) NOT VALID;
ALTER TABLE document ADD CONSTRAINT document_attachment_id_hh_fkey FOREIGN KEY (household_id, attachment_id) REFERENCES attachment (household_id, id) NOT VALID;
ALTER TABLE document ADD CONSTRAINT document_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE budget ADD CONSTRAINT budget_category_id_hh_fkey FOREIGN KEY (household_id, category_id) REFERENCES category (household_id, id) NOT VALID;
ALTER TABLE known_account ADD CONSTRAINT known_account_wealth_account_id_hh_fkey FOREIGN KEY (household_id, wealth_account_id) REFERENCES wealth_account (household_id, id) NOT VALID;
ALTER TABLE review_item ADD CONSTRAINT review_item_cycle_residual_case_id_hh_fkey FOREIGN KEY (household_id, cycle_residual_case_id) REFERENCES cycle_residual_case (household_id, id) NOT VALID;
ALTER TABLE review_item ADD CONSTRAINT review_item_document_id_hh_fkey FOREIGN KEY (document_id, household_id) REFERENCES document (id, household_id) NOT VALID;
ALTER TABLE review_item ADD CONSTRAINT review_item_financial_email_observation_id_hh_fkey FOREIGN KEY (household_id, financial_email_observation_id) REFERENCES financial_email_observation (household_id, id) NOT VALID;
ALTER TABLE review_item ADD CONSTRAINT review_item_proposal_id_hh_fkey FOREIGN KEY (household_id, proposal_id) REFERENCES transaction_proposal (household_id, id) NOT VALID;
ALTER TABLE review_item ADD CONSTRAINT review_item_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE review_item ADD CONSTRAINT review_item_transaction_id_hh_fkey FOREIGN KEY (household_id, transaction_id) REFERENCES transaction (household_id, id) NOT VALID;
ALTER TABLE review_item ADD CONSTRAINT review_item_wealth_observation_id_hh_fkey FOREIGN KEY (household_id, wealth_observation_id) REFERENCES wealth_observation (household_id, id) NOT VALID;
ALTER TABLE telegram_pending_action ADD CONSTRAINT telegram_pending_action_proposed_category_id_hh_fkey FOREIGN KEY (household_id, proposed_category_id) REFERENCES category (household_id, id) NOT VALID;
ALTER TABLE telegram_pending_action ADD CONSTRAINT telegram_pending_action_transaction_id_hh_fkey FOREIGN KEY (household_id, transaction_id) REFERENCES transaction (household_id, id) NOT VALID;
ALTER TABLE telegram_pending_batch ADD CONSTRAINT telegram_pending_batch_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE salary_event ADD CONSTRAINT salary_event_salary_source_id_hh_fkey FOREIGN KEY (household_id, salary_source_id) REFERENCES salary_source (household_id, id) NOT VALID;
ALTER TABLE salary_event ADD CONSTRAINT salary_event_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE salary_event ADD CONSTRAINT salary_event_transaction_id_hh_fkey FOREIGN KEY (household_id, transaction_id) REFERENCES transaction (household_id, id) NOT VALID;
ALTER TABLE salary_pending_choice ADD CONSTRAINT salary_pending_choice_transaction_id_hh_fkey FOREIGN KEY (household_id, transaction_id) REFERENCES transaction (household_id, id) NOT VALID;
ALTER TABLE bank_email_listener ADD CONSTRAINT bank_email_listener_account_id_hh_fkey FOREIGN KEY (household_id, account_id) REFERENCES account (household_id, id) NOT VALID;
ALTER TABLE telegram_conversation_turn ADD CONSTRAINT telegram_conversation_turn_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE telegram_turn_reference ADD CONSTRAINT telegram_turn_reference_turn_id_hh_fkey FOREIGN KEY (household_id, turn_id) REFERENCES telegram_conversation_turn (household_id, id) ON DELETE CASCADE NOT VALID;
ALTER TABLE email_ingress_delivery ADD CONSTRAINT email_ingress_delivery_address_id_hh_fkey FOREIGN KEY (household_id, address_id) REFERENCES email_ingress_address (household_id, id) NOT VALID;
ALTER TABLE email_ingress_delivery ADD CONSTRAINT email_ingress_delivery_financial_source_id_hh_fkey FOREIGN KEY (household_id, financial_source_id) REFERENCES financial_email_source (household_id, id) NOT VALID;
ALTER TABLE email_ingress_delivery ADD CONSTRAINT email_ingress_delivery_listener_id_hh_fkey FOREIGN KEY (household_id, listener_id) REFERENCES bank_email_listener (household_id, id) NOT VALID;
ALTER TABLE email_ingress_delivery ADD CONSTRAINT email_ingress_delivery_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE integration_action ADD CONSTRAINT integration_action_source_delivery_id_hh_fkey FOREIGN KEY (household_id, source_delivery_id) REFERENCES email_ingress_delivery (household_id, id) NOT VALID;
ALTER TABLE wealth_account ADD CONSTRAINT wealth_account_linked_account_id_hh_fkey FOREIGN KEY (household_id, linked_account_id) REFERENCES account (household_id, id) NOT VALID;
ALTER TABLE cycle_residual_case ADD CONSTRAINT cycle_residual_case_end_salary_event_id_hh_fkey FOREIGN KEY (household_id, end_salary_event_id) REFERENCES salary_event (household_id, id) NOT VALID;
ALTER TABLE cycle_residual_case ADD CONSTRAINT cycle_residual_case_start_salary_event_id_hh_fkey FOREIGN KEY (household_id, start_salary_event_id) REFERENCES salary_event (household_id, id) NOT VALID;
ALTER TABLE wealth_observation ADD CONSTRAINT wealth_observation_document_id_hh_fkey FOREIGN KEY (document_id, household_id) REFERENCES document (id, household_id) NOT VALID;
ALTER TABLE wealth_observation ADD CONSTRAINT wealth_observation_financial_email_observation_id_hh_fkey FOREIGN KEY (household_id, financial_email_observation_id) REFERENCES financial_email_observation (household_id, id) NOT VALID;
ALTER TABLE wealth_observation ADD CONSTRAINT wealth_observation_resolved_wealth_account_id_hh_fkey FOREIGN KEY (household_id, resolved_wealth_account_id) REFERENCES wealth_account (household_id, id) NOT VALID;
ALTER TABLE transfer_reconciliation_case ADD CONSTRAINT transfer_reconciliation_case_account_id_hh_fkey FOREIGN KEY (household_id, account_id) REFERENCES account (household_id, id) NOT VALID;
ALTER TABLE transfer_reconciliation_case ADD CONSTRAINT transfer_reconciliation_case_financial_email_obser_226e_hh_fkey FOREIGN KEY (household_id, financial_email_observation_id) REFERENCES financial_email_observation (household_id, id) NOT VALID;
ALTER TABLE transfer_reconciliation_case ADD CONSTRAINT transfer_reconciliation_case_proposed_wealth_account_id_hh_fkey FOREIGN KEY (household_id, proposed_wealth_account_id) REFERENCES wealth_account (household_id, id) NOT VALID;
ALTER TABLE transfer_reconciliation_case ADD CONSTRAINT transfer_reconciliation_case_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE financial_email_source ADD CONSTRAINT financial_email_source_default_wealth_account_id_hh_fkey FOREIGN KEY (household_id, default_wealth_account_id) REFERENCES wealth_account (household_id, id) NOT VALID;
ALTER TABLE email_sender_route ADD CONSTRAINT email_sender_route_bank_listener_id_hh_fkey FOREIGN KEY (household_id, bank_listener_id) REFERENCES bank_email_listener (household_id, id) NOT VALID;
ALTER TABLE email_sender_route ADD CONSTRAINT email_sender_route_financial_source_id_hh_fkey FOREIGN KEY (household_id, financial_source_id) REFERENCES financial_email_source (household_id, id) NOT VALID;
ALTER TABLE financial_entity_alias ADD CONSTRAINT financial_entity_alias_account_id_hh_fkey FOREIGN KEY (household_id, account_id) REFERENCES account (household_id, id) NOT VALID;
ALTER TABLE financial_entity_alias ADD CONSTRAINT financial_entity_alias_wealth_account_id_hh_fkey FOREIGN KEY (household_id, wealth_account_id) REFERENCES wealth_account (household_id, id) NOT VALID;
ALTER TABLE financial_email_observation ADD CONSTRAINT financial_email_observation_resolved_account_id_hh_fkey FOREIGN KEY (household_id, resolved_account_id) REFERENCES account (household_id, id) NOT VALID;
ALTER TABLE financial_email_observation ADD CONSTRAINT financial_email_observation_resolved_wealth_account_id_hh_fkey FOREIGN KEY (household_id, resolved_wealth_account_id) REFERENCES wealth_account (household_id, id) NOT VALID;
ALTER TABLE financial_email_observation ADD CONSTRAINT financial_email_observation_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE financial_email_observation ADD CONSTRAINT financial_email_observation_transaction_id_hh_fkey FOREIGN KEY (household_id, transaction_id) REFERENCES transaction (household_id, id) NOT VALID;
ALTER TABLE financial_email_observation ADD CONSTRAINT financial_email_observation_wealth_observation_id_hh_fkey FOREIGN KEY (household_id, wealth_observation_id) REFERENCES wealth_observation (household_id, id) NOT VALID;
ALTER TABLE financial_email_preview ADD CONSTRAINT financial_email_preview_financial_source_id_hh_fkey FOREIGN KEY (household_id, financial_source_id) REFERENCES financial_email_source (household_id, id) NOT VALID;
ALTER TABLE judgment_decision ADD CONSTRAINT judgment_decision_source_event_id_hh_fkey FOREIGN KEY (household_id, source_event_id) REFERENCES source_event (household_id, id) NOT VALID;
ALTER TABLE category VALIDATE CONSTRAINT category_parent_id_hh_fkey;
ALTER TABLE merchant_alias VALIDATE CONSTRAINT merchant_alias_default_category_id_hh_fkey;
ALTER TABLE merchant_alias VALIDATE CONSTRAINT merchant_alias_normalized_merchant_id_hh_fkey;
ALTER TABLE transaction VALIDATE CONSTRAINT transaction_account_id_hh_fkey;
ALTER TABLE transaction VALIDATE CONSTRAINT transaction_category_id_hh_fkey;
ALTER TABLE transaction VALIDATE CONSTRAINT transaction_merchant_id_hh_fkey;
ALTER TABLE transaction VALIDATE CONSTRAINT transaction_related_wealth_account_id_hh_fkey;
ALTER TABLE transaction_proposal VALIDATE CONSTRAINT transaction_proposal_category_candidate_id_hh_fkey;
ALTER TABLE transaction_proposal VALIDATE CONSTRAINT transaction_proposal_source_event_id_hh_fkey;
ALTER TABLE reconciliation_merge VALIDATE CONSTRAINT reconciliation_merge_source_transaction_id_hh_fkey;
ALTER TABLE reconciliation_merge VALIDATE CONSTRAINT reconciliation_merge_target_transaction_id_hh_fkey;
ALTER TABLE review_request VALIDATE CONSTRAINT review_request_review_item_id_hh_fkey;
ALTER TABLE review_request VALIDATE CONSTRAINT review_request_transaction_id_hh_fkey;
ALTER TABLE document VALIDATE CONSTRAINT document_attachment_id_hh_fkey;
ALTER TABLE document VALIDATE CONSTRAINT document_source_event_id_hh_fkey;
ALTER TABLE budget VALIDATE CONSTRAINT budget_category_id_hh_fkey;
ALTER TABLE known_account VALIDATE CONSTRAINT known_account_wealth_account_id_hh_fkey;
ALTER TABLE review_item VALIDATE CONSTRAINT review_item_cycle_residual_case_id_hh_fkey;
ALTER TABLE review_item VALIDATE CONSTRAINT review_item_document_id_hh_fkey;
ALTER TABLE review_item VALIDATE CONSTRAINT review_item_financial_email_observation_id_hh_fkey;
ALTER TABLE review_item VALIDATE CONSTRAINT review_item_proposal_id_hh_fkey;
ALTER TABLE review_item VALIDATE CONSTRAINT review_item_source_event_id_hh_fkey;
ALTER TABLE review_item VALIDATE CONSTRAINT review_item_transaction_id_hh_fkey;
ALTER TABLE review_item VALIDATE CONSTRAINT review_item_wealth_observation_id_hh_fkey;
ALTER TABLE telegram_pending_action VALIDATE CONSTRAINT telegram_pending_action_proposed_category_id_hh_fkey;
ALTER TABLE telegram_pending_action VALIDATE CONSTRAINT telegram_pending_action_transaction_id_hh_fkey;
ALTER TABLE telegram_pending_batch VALIDATE CONSTRAINT telegram_pending_batch_source_event_id_hh_fkey;
ALTER TABLE salary_event VALIDATE CONSTRAINT salary_event_salary_source_id_hh_fkey;
ALTER TABLE salary_event VALIDATE CONSTRAINT salary_event_source_event_id_hh_fkey;
ALTER TABLE salary_event VALIDATE CONSTRAINT salary_event_transaction_id_hh_fkey;
ALTER TABLE salary_pending_choice VALIDATE CONSTRAINT salary_pending_choice_transaction_id_hh_fkey;
ALTER TABLE bank_email_listener VALIDATE CONSTRAINT bank_email_listener_account_id_hh_fkey;
ALTER TABLE telegram_conversation_turn VALIDATE CONSTRAINT telegram_conversation_turn_source_event_id_hh_fkey;
ALTER TABLE telegram_turn_reference VALIDATE CONSTRAINT telegram_turn_reference_turn_id_hh_fkey;
ALTER TABLE email_ingress_delivery VALIDATE CONSTRAINT email_ingress_delivery_address_id_hh_fkey;
ALTER TABLE email_ingress_delivery VALIDATE CONSTRAINT email_ingress_delivery_financial_source_id_hh_fkey;
ALTER TABLE email_ingress_delivery VALIDATE CONSTRAINT email_ingress_delivery_listener_id_hh_fkey;
ALTER TABLE email_ingress_delivery VALIDATE CONSTRAINT email_ingress_delivery_source_event_id_hh_fkey;
ALTER TABLE integration_action VALIDATE CONSTRAINT integration_action_source_delivery_id_hh_fkey;
ALTER TABLE wealth_account VALIDATE CONSTRAINT wealth_account_linked_account_id_hh_fkey;
ALTER TABLE cycle_residual_case VALIDATE CONSTRAINT cycle_residual_case_end_salary_event_id_hh_fkey;
ALTER TABLE cycle_residual_case VALIDATE CONSTRAINT cycle_residual_case_start_salary_event_id_hh_fkey;
ALTER TABLE wealth_observation VALIDATE CONSTRAINT wealth_observation_document_id_hh_fkey;
ALTER TABLE wealth_observation VALIDATE CONSTRAINT wealth_observation_financial_email_observation_id_hh_fkey;
ALTER TABLE wealth_observation VALIDATE CONSTRAINT wealth_observation_resolved_wealth_account_id_hh_fkey;
ALTER TABLE transfer_reconciliation_case VALIDATE CONSTRAINT transfer_reconciliation_case_account_id_hh_fkey;
ALTER TABLE transfer_reconciliation_case VALIDATE CONSTRAINT transfer_reconciliation_case_financial_email_obser_226e_hh_fkey;
ALTER TABLE transfer_reconciliation_case VALIDATE CONSTRAINT transfer_reconciliation_case_proposed_wealth_account_id_hh_fkey;
ALTER TABLE transfer_reconciliation_case VALIDATE CONSTRAINT transfer_reconciliation_case_source_event_id_hh_fkey;
ALTER TABLE financial_email_source VALIDATE CONSTRAINT financial_email_source_default_wealth_account_id_hh_fkey;
ALTER TABLE email_sender_route VALIDATE CONSTRAINT email_sender_route_bank_listener_id_hh_fkey;
ALTER TABLE email_sender_route VALIDATE CONSTRAINT email_sender_route_financial_source_id_hh_fkey;
ALTER TABLE financial_entity_alias VALIDATE CONSTRAINT financial_entity_alias_account_id_hh_fkey;
ALTER TABLE financial_entity_alias VALIDATE CONSTRAINT financial_entity_alias_wealth_account_id_hh_fkey;
ALTER TABLE financial_email_observation VALIDATE CONSTRAINT financial_email_observation_resolved_account_id_hh_fkey;
ALTER TABLE financial_email_observation VALIDATE CONSTRAINT financial_email_observation_resolved_wealth_account_id_hh_fkey;
ALTER TABLE financial_email_observation VALIDATE CONSTRAINT financial_email_observation_source_event_id_hh_fkey;
ALTER TABLE financial_email_observation VALIDATE CONSTRAINT financial_email_observation_transaction_id_hh_fkey;
ALTER TABLE financial_email_observation VALIDATE CONSTRAINT financial_email_observation_wealth_observation_id_hh_fkey;
ALTER TABLE financial_email_preview VALIDATE CONSTRAINT financial_email_preview_financial_source_id_hh_fkey;
ALTER TABLE judgment_decision VALIDATE CONSTRAINT judgment_decision_source_event_id_hh_fkey;

-- +goose Down
ALTER TABLE judgment_decision DROP CONSTRAINT IF EXISTS judgment_decision_source_event_id_hh_fkey;
ALTER TABLE financial_email_preview DROP CONSTRAINT IF EXISTS financial_email_preview_financial_source_id_hh_fkey;
ALTER TABLE financial_email_observation DROP CONSTRAINT IF EXISTS financial_email_observation_wealth_observation_id_hh_fkey;
ALTER TABLE financial_email_observation DROP CONSTRAINT IF EXISTS financial_email_observation_transaction_id_hh_fkey;
ALTER TABLE financial_email_observation DROP CONSTRAINT IF EXISTS financial_email_observation_source_event_id_hh_fkey;
ALTER TABLE financial_email_observation DROP CONSTRAINT IF EXISTS financial_email_observation_resolved_wealth_account_id_hh_fkey;
ALTER TABLE financial_email_observation DROP CONSTRAINT IF EXISTS financial_email_observation_resolved_account_id_hh_fkey;
ALTER TABLE financial_entity_alias DROP CONSTRAINT IF EXISTS financial_entity_alias_wealth_account_id_hh_fkey;
ALTER TABLE financial_entity_alias DROP CONSTRAINT IF EXISTS financial_entity_alias_account_id_hh_fkey;
ALTER TABLE email_sender_route DROP CONSTRAINT IF EXISTS email_sender_route_financial_source_id_hh_fkey;
ALTER TABLE email_sender_route DROP CONSTRAINT IF EXISTS email_sender_route_bank_listener_id_hh_fkey;
ALTER TABLE financial_email_source DROP CONSTRAINT IF EXISTS financial_email_source_default_wealth_account_id_hh_fkey;
ALTER TABLE transfer_reconciliation_case DROP CONSTRAINT IF EXISTS transfer_reconciliation_case_source_event_id_hh_fkey;
ALTER TABLE transfer_reconciliation_case DROP CONSTRAINT IF EXISTS transfer_reconciliation_case_proposed_wealth_account_id_hh_fkey;
ALTER TABLE transfer_reconciliation_case DROP CONSTRAINT IF EXISTS transfer_reconciliation_case_financial_email_obser_226e_hh_fkey;
ALTER TABLE transfer_reconciliation_case DROP CONSTRAINT IF EXISTS transfer_reconciliation_case_account_id_hh_fkey;
ALTER TABLE wealth_observation DROP CONSTRAINT IF EXISTS wealth_observation_resolved_wealth_account_id_hh_fkey;
ALTER TABLE wealth_observation DROP CONSTRAINT IF EXISTS wealth_observation_financial_email_observation_id_hh_fkey;
ALTER TABLE wealth_observation DROP CONSTRAINT IF EXISTS wealth_observation_document_id_hh_fkey;
ALTER TABLE cycle_residual_case DROP CONSTRAINT IF EXISTS cycle_residual_case_start_salary_event_id_hh_fkey;
ALTER TABLE cycle_residual_case DROP CONSTRAINT IF EXISTS cycle_residual_case_end_salary_event_id_hh_fkey;
ALTER TABLE wealth_account DROP CONSTRAINT IF EXISTS wealth_account_linked_account_id_hh_fkey;
ALTER TABLE integration_action DROP CONSTRAINT IF EXISTS integration_action_source_delivery_id_hh_fkey;
ALTER TABLE email_ingress_delivery DROP CONSTRAINT IF EXISTS email_ingress_delivery_source_event_id_hh_fkey;
ALTER TABLE email_ingress_delivery DROP CONSTRAINT IF EXISTS email_ingress_delivery_listener_id_hh_fkey;
ALTER TABLE email_ingress_delivery DROP CONSTRAINT IF EXISTS email_ingress_delivery_financial_source_id_hh_fkey;
ALTER TABLE email_ingress_delivery DROP CONSTRAINT IF EXISTS email_ingress_delivery_address_id_hh_fkey;
ALTER TABLE telegram_turn_reference DROP CONSTRAINT IF EXISTS telegram_turn_reference_turn_id_hh_fkey;
ALTER TABLE telegram_conversation_turn DROP CONSTRAINT IF EXISTS telegram_conversation_turn_source_event_id_hh_fkey;
ALTER TABLE bank_email_listener DROP CONSTRAINT IF EXISTS bank_email_listener_account_id_hh_fkey;
ALTER TABLE salary_pending_choice DROP CONSTRAINT IF EXISTS salary_pending_choice_transaction_id_hh_fkey;
ALTER TABLE salary_event DROP CONSTRAINT IF EXISTS salary_event_transaction_id_hh_fkey;
ALTER TABLE salary_event DROP CONSTRAINT IF EXISTS salary_event_source_event_id_hh_fkey;
ALTER TABLE salary_event DROP CONSTRAINT IF EXISTS salary_event_salary_source_id_hh_fkey;
ALTER TABLE telegram_pending_batch DROP CONSTRAINT IF EXISTS telegram_pending_batch_source_event_id_hh_fkey;
ALTER TABLE telegram_pending_action DROP CONSTRAINT IF EXISTS telegram_pending_action_transaction_id_hh_fkey;
ALTER TABLE telegram_pending_action DROP CONSTRAINT IF EXISTS telegram_pending_action_proposed_category_id_hh_fkey;
ALTER TABLE review_item DROP CONSTRAINT IF EXISTS review_item_wealth_observation_id_hh_fkey;
ALTER TABLE review_item DROP CONSTRAINT IF EXISTS review_item_transaction_id_hh_fkey;
ALTER TABLE review_item DROP CONSTRAINT IF EXISTS review_item_source_event_id_hh_fkey;
ALTER TABLE review_item DROP CONSTRAINT IF EXISTS review_item_proposal_id_hh_fkey;
ALTER TABLE review_item DROP CONSTRAINT IF EXISTS review_item_financial_email_observation_id_hh_fkey;
ALTER TABLE review_item DROP CONSTRAINT IF EXISTS review_item_document_id_hh_fkey;
ALTER TABLE review_item DROP CONSTRAINT IF EXISTS review_item_cycle_residual_case_id_hh_fkey;
ALTER TABLE known_account DROP CONSTRAINT IF EXISTS known_account_wealth_account_id_hh_fkey;
ALTER TABLE budget DROP CONSTRAINT IF EXISTS budget_category_id_hh_fkey;
ALTER TABLE document DROP CONSTRAINT IF EXISTS document_source_event_id_hh_fkey;
ALTER TABLE document DROP CONSTRAINT IF EXISTS document_attachment_id_hh_fkey;
ALTER TABLE review_request DROP CONSTRAINT IF EXISTS review_request_transaction_id_hh_fkey;
ALTER TABLE review_request DROP CONSTRAINT IF EXISTS review_request_review_item_id_hh_fkey;
ALTER TABLE reconciliation_merge DROP CONSTRAINT IF EXISTS reconciliation_merge_target_transaction_id_hh_fkey;
ALTER TABLE reconciliation_merge DROP CONSTRAINT IF EXISTS reconciliation_merge_source_transaction_id_hh_fkey;
ALTER TABLE transaction_proposal DROP CONSTRAINT IF EXISTS transaction_proposal_source_event_id_hh_fkey;
ALTER TABLE transaction_proposal DROP CONSTRAINT IF EXISTS transaction_proposal_category_candidate_id_hh_fkey;
ALTER TABLE transaction DROP CONSTRAINT IF EXISTS transaction_related_wealth_account_id_hh_fkey;
ALTER TABLE transaction DROP CONSTRAINT IF EXISTS transaction_merchant_id_hh_fkey;
ALTER TABLE transaction DROP CONSTRAINT IF EXISTS transaction_category_id_hh_fkey;
ALTER TABLE transaction DROP CONSTRAINT IF EXISTS transaction_account_id_hh_fkey;
ALTER TABLE merchant_alias DROP CONSTRAINT IF EXISTS merchant_alias_normalized_merchant_id_hh_fkey;
ALTER TABLE merchant_alias DROP CONSTRAINT IF EXISTS merchant_alias_default_category_id_hh_fkey;
ALTER TABLE category DROP CONSTRAINT IF EXISTS category_parent_id_hh_fkey;
DROP INDEX IF EXISTS account_household_id_id_key;
DROP INDEX IF EXISTS attachment_household_id_id_key;
DROP INDEX IF EXISTS bank_email_listener_household_id_id_key;
DROP INDEX IF EXISTS category_household_id_id_key;
DROP INDEX IF EXISTS cycle_residual_case_household_id_id_key;
DROP INDEX IF EXISTS email_ingress_address_household_id_id_key;
DROP INDEX IF EXISTS email_ingress_delivery_household_id_id_key;
DROP INDEX IF EXISTS financial_email_observation_household_id_id_key;
DROP INDEX IF EXISTS financial_email_source_household_id_id_key;
DROP INDEX IF EXISTS merchant_household_id_id_key;
DROP INDEX IF EXISTS review_item_household_id_id_key;
DROP INDEX IF EXISTS salary_event_household_id_id_key;
DROP INDEX IF EXISTS salary_source_household_id_id_key;
DROP INDEX IF EXISTS source_event_household_id_id_key;
DROP INDEX IF EXISTS telegram_conversation_turn_household_id_id_key;
DROP INDEX IF EXISTS transaction_household_id_id_key;
DROP INDEX IF EXISTS transaction_proposal_household_id_id_key;
DROP INDEX IF EXISTS wealth_account_household_id_id_key;
DROP INDEX IF EXISTS wealth_observation_household_id_id_key;
DROP INDEX IF EXISTS job_lane_household_started_idx;
DROP INDEX IF EXISTS job_household_status_idx;
DROP TRIGGER IF EXISTS job_assign_household ON job;
DROP FUNCTION IF EXISTS assign_job_household();
DROP FUNCTION IF EXISTS job_household(JSONB);
DROP FUNCTION IF EXISTS job_payload_uuid(JSONB, TEXT);
ALTER TABLE job DROP COLUMN IF EXISTS household_id;
