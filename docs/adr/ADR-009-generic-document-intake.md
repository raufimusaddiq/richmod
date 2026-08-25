# ADR-009: Generic finance document intake

## Status

Accepted.

## Decision

All finance images and documents enter one validated attachment pipeline and then
produce proposals for deterministic validation and reconciliation.

The first production increment accepts JPEG and PNG images up to 10 MiB and 24
megapixels from the authenticated web API. Classification uses the cloud gateway
with a strict allowlisted schema; low-confidence and generic financial documents
enter Review instead of creating ledger mutations. Type-specific extraction is
handled by subsequent pipeline stages.

Payslips use a second strict vision extraction stage. Go accepts only whole IDR,
validates the payroll period, positive net pay, gross-versus-net ordering, optional
pay date, and available arithmetic. High-confidence arithmetically consistent
payslips with a pay date may confirm one net-pay income; all others enter Review.
Allowances and deductions remain extraction metadata and never create expenses.
