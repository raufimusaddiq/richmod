-- Read-only pre-check for migration 00087 (ADR-058).
-- Lists every same-household foreign key the migration will validate and how
-- many existing rows would fail it. Run before deploying; every row must show
-- 0 violations. Any non-zero count is real cross-household data that must be
-- investigated and corrected deliberately: the migration never repairs it.
--
--   psql "$DATABASE_URL" -f scripts/check-cross-household-references.sql
SELECT constraint_name, violations FROM (
SELECT 'category_parent_id_hh_fkey' AS constraint_name, count(*) AS violations FROM category c JOIN category p ON p.id = c.parent_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'merchant_alias_default_category_id_hh_fkey' AS constraint_name, count(*) AS violations FROM merchant_alias c JOIN category p ON p.id = c.default_category_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'merchant_alias_normalized_merchant_id_hh_fkey' AS constraint_name, count(*) AS violations FROM merchant_alias c JOIN merchant p ON p.id = c.normalized_merchant_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transaction_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM transaction c JOIN account p ON p.id = c.account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transaction_category_id_hh_fkey' AS constraint_name, count(*) AS violations FROM transaction c JOIN category p ON p.id = c.category_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transaction_merchant_id_hh_fkey' AS constraint_name, count(*) AS violations FROM transaction c JOIN merchant p ON p.id = c.merchant_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transaction_related_wealth_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM transaction c JOIN wealth_account p ON p.id = c.related_wealth_account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transaction_proposal_category_candidate_id_hh_fkey' AS constraint_name, count(*) AS violations FROM transaction_proposal c JOIN category p ON p.id = c.category_candidate_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transaction_proposal_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM transaction_proposal c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'reconciliation_merge_source_transaction_id_hh_fkey' AS constraint_name, count(*) AS violations FROM reconciliation_merge c JOIN transaction p ON p.id = c.source_transaction_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'reconciliation_merge_target_transaction_id_hh_fkey' AS constraint_name, count(*) AS violations FROM reconciliation_merge c JOIN transaction p ON p.id = c.target_transaction_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'review_request_review_item_id_hh_fkey' AS constraint_name, count(*) AS violations FROM review_request c JOIN review_item p ON p.id = c.review_item_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'review_request_transaction_id_hh_fkey' AS constraint_name, count(*) AS violations FROM review_request c JOIN transaction p ON p.id = c.transaction_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'document_attachment_id_hh_fkey' AS constraint_name, count(*) AS violations FROM document c JOIN attachment p ON p.id = c.attachment_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'document_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM document c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'budget_category_id_hh_fkey' AS constraint_name, count(*) AS violations FROM budget c JOIN category p ON p.id = c.category_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'known_account_wealth_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM known_account c JOIN wealth_account p ON p.id = c.wealth_account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'review_item_cycle_residual_case_id_hh_fkey' AS constraint_name, count(*) AS violations FROM review_item c JOIN cycle_residual_case p ON p.id = c.cycle_residual_case_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'review_item_document_id_hh_fkey' AS constraint_name, count(*) AS violations FROM review_item c JOIN document p ON p.id = c.document_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'review_item_financial_email_observation_id_hh_fkey' AS constraint_name, count(*) AS violations FROM review_item c JOIN financial_email_observation p ON p.id = c.financial_email_observation_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'review_item_proposal_id_hh_fkey' AS constraint_name, count(*) AS violations FROM review_item c JOIN transaction_proposal p ON p.id = c.proposal_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'review_item_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM review_item c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'review_item_transaction_id_hh_fkey' AS constraint_name, count(*) AS violations FROM review_item c JOIN transaction p ON p.id = c.transaction_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'review_item_wealth_observation_id_hh_fkey' AS constraint_name, count(*) AS violations FROM review_item c JOIN wealth_observation p ON p.id = c.wealth_observation_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'telegram_pending_action_proposed_category_id_hh_fkey' AS constraint_name, count(*) AS violations FROM telegram_pending_action c JOIN category p ON p.id = c.proposed_category_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'telegram_pending_action_transaction_id_hh_fkey' AS constraint_name, count(*) AS violations FROM telegram_pending_action c JOIN transaction p ON p.id = c.transaction_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'telegram_pending_batch_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM telegram_pending_batch c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'salary_event_salary_source_id_hh_fkey' AS constraint_name, count(*) AS violations FROM salary_event c JOIN salary_source p ON p.id = c.salary_source_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'salary_event_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM salary_event c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'salary_event_transaction_id_hh_fkey' AS constraint_name, count(*) AS violations FROM salary_event c JOIN transaction p ON p.id = c.transaction_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'salary_pending_choice_transaction_id_hh_fkey' AS constraint_name, count(*) AS violations FROM salary_pending_choice c JOIN transaction p ON p.id = c.transaction_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'bank_email_listener_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM bank_email_listener c JOIN account p ON p.id = c.account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'telegram_conversation_turn_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM telegram_conversation_turn c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'telegram_turn_reference_turn_id_hh_fkey' AS constraint_name, count(*) AS violations FROM telegram_turn_reference c JOIN telegram_conversation_turn p ON p.id = c.turn_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'email_ingress_delivery_address_id_hh_fkey' AS constraint_name, count(*) AS violations FROM email_ingress_delivery c JOIN email_ingress_address p ON p.id = c.address_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'email_ingress_delivery_financial_source_id_hh_fkey' AS constraint_name, count(*) AS violations FROM email_ingress_delivery c JOIN financial_email_source p ON p.id = c.financial_source_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'email_ingress_delivery_listener_id_hh_fkey' AS constraint_name, count(*) AS violations FROM email_ingress_delivery c JOIN bank_email_listener p ON p.id = c.listener_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'email_ingress_delivery_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM email_ingress_delivery c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'integration_action_source_delivery_id_hh_fkey' AS constraint_name, count(*) AS violations FROM integration_action c JOIN email_ingress_delivery p ON p.id = c.source_delivery_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'wealth_account_linked_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM wealth_account c JOIN account p ON p.id = c.linked_account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'cycle_residual_case_end_salary_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM cycle_residual_case c JOIN salary_event p ON p.id = c.end_salary_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'cycle_residual_case_start_salary_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM cycle_residual_case c JOIN salary_event p ON p.id = c.start_salary_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'wealth_observation_document_id_hh_fkey' AS constraint_name, count(*) AS violations FROM wealth_observation c JOIN document p ON p.id = c.document_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'wealth_observation_financial_email_observation_id_hh_fkey' AS constraint_name, count(*) AS violations FROM wealth_observation c JOIN financial_email_observation p ON p.id = c.financial_email_observation_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'wealth_observation_resolved_wealth_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM wealth_observation c JOIN wealth_account p ON p.id = c.resolved_wealth_account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transfer_reconciliation_case_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM transfer_reconciliation_case c JOIN account p ON p.id = c.account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transfer_reconciliation_case_financial_email_obser_226e_hh_fkey' AS constraint_name, count(*) AS violations FROM transfer_reconciliation_case c JOIN financial_email_observation p ON p.id = c.financial_email_observation_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transfer_reconciliation_case_proposed_wealth_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM transfer_reconciliation_case c JOIN wealth_account p ON p.id = c.proposed_wealth_account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'transfer_reconciliation_case_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM transfer_reconciliation_case c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'financial_email_source_default_wealth_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM financial_email_source c JOIN wealth_account p ON p.id = c.default_wealth_account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'email_sender_route_bank_listener_id_hh_fkey' AS constraint_name, count(*) AS violations FROM email_sender_route c JOIN bank_email_listener p ON p.id = c.bank_listener_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'email_sender_route_financial_source_id_hh_fkey' AS constraint_name, count(*) AS violations FROM email_sender_route c JOIN financial_email_source p ON p.id = c.financial_source_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'financial_entity_alias_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM financial_entity_alias c JOIN account p ON p.id = c.account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'financial_entity_alias_wealth_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM financial_entity_alias c JOIN wealth_account p ON p.id = c.wealth_account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'financial_email_observation_resolved_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM financial_email_observation c JOIN account p ON p.id = c.resolved_account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'financial_email_observation_resolved_wealth_account_id_hh_fkey' AS constraint_name, count(*) AS violations FROM financial_email_observation c JOIN wealth_account p ON p.id = c.resolved_wealth_account_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'financial_email_observation_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM financial_email_observation c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'financial_email_observation_transaction_id_hh_fkey' AS constraint_name, count(*) AS violations FROM financial_email_observation c JOIN transaction p ON p.id = c.transaction_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'financial_email_observation_wealth_observation_id_hh_fkey' AS constraint_name, count(*) AS violations FROM financial_email_observation c JOIN wealth_observation p ON p.id = c.wealth_observation_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'financial_email_preview_financial_source_id_hh_fkey' AS constraint_name, count(*) AS violations FROM financial_email_preview c JOIN financial_email_source p ON p.id = c.financial_source_id WHERE c.household_id IS DISTINCT FROM p.household_id
UNION ALL
SELECT 'judgment_decision_source_event_id_hh_fkey' AS constraint_name, count(*) AS violations FROM judgment_decision c JOIN source_event p ON p.id = c.source_event_id WHERE c.household_id IS DISTINCT FROM p.household_id
) checks
ORDER BY violations DESC, constraint_name;
