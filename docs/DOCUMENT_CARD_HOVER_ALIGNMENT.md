# Document Card Hover Alignment

Document cards use the same neutral hover surface as the left navigation rather than the stronger white surface.

- Hover background: `var(--surface-muted)`.
- Existing document-card border and text treatment remain unchanged.
- Click behavior, document detail routing, and active-state transform behavior are unchanged.
- A focused web test protects the interaction token from regressing to `var(--surface-strong)`.
