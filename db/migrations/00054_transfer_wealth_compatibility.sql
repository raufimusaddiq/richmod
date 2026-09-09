-- +goose Up
CREATE FUNCTION transfer_wealth_compatible(purpose TEXT, wealth_id UUID, household UUID)
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
AS $$
SELECT CASE
    WHEN purpose = 'INTERNAL_TRANSFER' THEN wealth_id IS NULL
    WHEN purpose = 'SAVINGS_TRANSFER' THEN EXISTS (SELECT 1 FROM wealth_account WHERE id=wealth_id AND household_id=household AND active AND side='ASSET' AND usage_role='SAVINGS')
    WHEN purpose = 'INVESTMENT_CONTRIBUTION' THEN EXISTS (SELECT 1 FROM wealth_account WHERE id=wealth_id AND household_id=household AND active AND side='ASSET' AND usage_role='INVESTMENT')
    WHEN purpose = 'ASSET_PURCHASE' THEN EXISTS (SELECT 1 FROM wealth_account WHERE id=wealth_id AND household_id=household AND active AND side='ASSET')
    WHEN purpose = 'DEBT_PRINCIPAL_PAYMENT' THEN EXISTS (SELECT 1 FROM wealth_account WHERE id=wealth_id AND household_id=household AND active AND side='LIABILITY')
    ELSE FALSE
END
$$;

-- +goose Down
DROP FUNCTION transfer_wealth_compatible(TEXT, UUID, UUID);
