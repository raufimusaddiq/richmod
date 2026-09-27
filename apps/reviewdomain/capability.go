package reviewdomain

// TelegramCompleteActions lists ReviewDecision ordinary actions with a Telegram
// terminal or continuation. Admin metrics and the worker contract gate share it.
func TelegramCompleteActions() []string {
	return []string{
		"CONFIRM_REVIEW", "CLASSIFY_TRANSFER", "ALLOCATE_RETAINED_BALANCE", "LEAVE_UNALLOCATED",
		"TRANSACTION_MISSING", "PRIMARY_SALARY", "ORDINARY_INCOME", "SET_PAY_DATE",
		"COMPLETE_BANK_FACTS", "REPROCESS_DOCUMENT", "SET_FINANCIAL_EMAIL_ENTITIES",
		"MERGE_EXISTING", "CONFIRM_NEW_TRANSFER", "SET_WEALTH_ACCOUNT", "RECORD_ASSET_PURCHASE",
		"OWN_ACCOUNT", "HOUSEHOLD_ACCOUNT", "INVESTMENT_ACCOUNT", "EXPENSE", "ASSET_PURCHASE",
	}
}
