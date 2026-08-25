# ADR-014: Monthly category budgets

## Status

Accepted.

## Decision

MVP budgets are recurring monthly whole-IDR limits scoped to one household expense
category. Owners manage limits; all household members may view progress. Spending
uses confirmed expenses minus confirmed refunds in `Asia/Jakarta` calendar months.
A parent category budget includes all descendant-category transactions. Budgets are
deactivated rather than deleted, and every mutation is audited.
