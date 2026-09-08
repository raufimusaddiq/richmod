# Richmod Open Graph Share Preview

## Scope

The public Richmod experience exposes social-sharing metadata from the root Next.js layout. This is presentation-only and does not change routes, authentication, financial state, API contracts, or evidence handling.

## Production metadata

- Canonical production origin for metadata asset resolution: `https://finance.investdx.biz.id`.
- Open Graph includes Richmod title, social description, site name, Indonesian locale, website type, and a 1200×630 preview image.
- Twitter/X uses `summary_large_image` with the same title, description, and preview image.
- The preview image is generated as PNG by `apps/web/app/opengraph-image.js` using the existing warm-neutral and dried-plum public brand language.
- The card contains product positioning only and must never render household, transaction, document, review, or authentication data.

## Verification

After deployment, verify the rendered page head contains absolute `og:image` and Twitter image metadata resolving under the production origin, and confirm the generated image endpoint returns `image/png` at 1200×630.

When testing share previews, remember that WhatsApp, Telegram, LinkedIn, and other crawlers may cache older metadata. Use their refresh/debug tooling or a cache-busting test URL when validating a newly deployed card.
