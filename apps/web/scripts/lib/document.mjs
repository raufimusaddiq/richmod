import { siteMeta, siteOrigin, themeColor } from "../../app/lib/siteMeta.js";
import { splashDevices } from "../splash-devices.mjs";

const escapeAttribute = value => String(value).replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
const escapeText = value => String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");

const meta = (attributes, content) => `<meta ${attributes} content="${escapeAttribute(content)}"/>`;

export function renderHead({ title, description, noindex = false, stylesheet, script, ogImageVersion }) {
  const { social } = siteMeta;
  const image = `${siteOrigin}${social.image.path}`;
  const tags = [
    '<meta charset="utf-8"/>',
    meta('name="viewport"', "width=device-width, initial-scale=1, viewport-fit=cover"),
    `<link rel="stylesheet" href="${stylesheet}"/>`,
    ...(noindex ? [meta('name="robots"', "noindex")] : []),
    meta('name="theme-color"', themeColor),
    `<title>${escapeText(title)}</title>`,
    meta('name="description"', description),
    '<link rel="manifest" href="/manifest.webmanifest"/>',
    meta('name="mobile-web-app-capable"', "yes"),
    meta('name="apple-mobile-web-app-title"', "Richmod"),
    ...splashDevices.map(device => `<link href="/splash/${device.file}" media="${escapeAttribute(device.media)}" rel="apple-touch-startup-image"/>`),
    meta('name="apple-mobile-web-app-status-bar-style"', "default"),
    meta('property="og:title"', social.title),
    meta('property="og:description"', social.description),
    meta('property="og:site_name"', social.siteName),
    meta('property="og:locale"', social.locale),
    meta('property="og:image"', `${image}?${ogImageVersion}`),
    meta('property="og:image:type"', "image/png"),
    meta('property="og:image:width"', String(social.image.width)),
    meta('property="og:image:height"', String(social.image.height)),
    meta('property="og:image:alt"', social.image.alt),
    meta('property="og:type"', "website"),
    meta('name="twitter:card"', "summary_large_image"),
    meta('name="twitter:title"', social.title),
    meta('name="twitter:description"', social.description),
    meta('name="twitter:image"', image),
    '<link rel="icon" href="/icon.svg" type="image/svg+xml"/>',
    '<link rel="apple-touch-icon" href="/icons/apple-touch-icon-180.png" sizes="180x180" type="image/png"/>',
    meta('name="apple-mobile-web-app-capable"', "yes"),
    `<script type="module" src="${script}"></script>`,
  ];
  return tags.join("");
}

export function renderDocument({ body, ...head }) {
  return `<!DOCTYPE html><html lang="id"><head>${renderHead(head)}</head><body><div id="root">${body}</div></body></html>`;
}
