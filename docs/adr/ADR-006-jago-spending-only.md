# ADR-006: Bank Jago SPENDING_ONLY policy

## Status

Accepted.

## Decision

Jago outgoing merchant, QR, and debit-card payments may be expense candidates.
An outgoing bank transfer is `UNCLASSIFIED` and excluded from analytics until a
deterministic known-account match or explicit review resolves it to EXPENSE,
TRANSFER, or IGNORE. It is never represented as EXPENSE merely because money left
Jago.

Household-scoped `known_account` records use a masked stable `match_hint`, not an
unnecessary full account number. `OWN_ACCOUNT` and `HOUSEHOLD_ACCOUNT` resolve to
confirmed TRANSFER; `INVESTMENT_ACCOUNT` resolves to a retained, voided
non-spending reference. Unknown and `OTHER` destinations remain in
`TRANSFER_CLASSIFICATION` review. Incoming funding, pocket movements, and RDN
movements do not become household income or spending.
