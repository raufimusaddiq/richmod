# Security policy

Richmod processes household financial evidence and should be treated as security-sensitive software.

## Reporting a vulnerability

Please do **not** open a public GitHub issue for vulnerabilities, suspected credential exposure, authentication bypasses, household-isolation failures, or other security-sensitive findings.

Use GitHub's private vulnerability reporting for this repository when available. If private reporting is unavailable, contact the repository owner privately through an established channel before publishing technical details.

A useful report includes:

- affected component and version or commit;
- reproduction steps;
- expected versus observed behavior;
- impact, especially whether another household's data can be read or mutated;
- whether secrets, tokens, raw financial evidence, or account data are exposed;
- any known mitigation.

Please avoid including real financial data, credentials, forwarding verification links, HMAC secrets, session tokens, or raw production email contents in the report unless a private channel has been established and the material is necessary.

## Security-sensitive areas

Extra care is expected around:

- household identity and authorization;
- email-ingress HMAC verification and recipient routing;
- Telegram identity and callback binding;
- session and enrollment tokens;
- document and raw-email storage;
- LLM tool boundaries and deterministic validation;
- financial-state transitions, reconciliation, and review resolution;
- audit/evidence retention;
- deployment secrets and backup credentials.

## Supported version

Security fixes target the current `main` branch and the currently deployed production line. Historical commits and superseded deployment paths are not maintained as separate supported releases.
