import { MARKETING_ORIGIN } from "./app-origin";

/** Public path to the canonical Fact0 wordmark (`web/public/logo.svg`). */
export const BRAND_LOGO_PATH = "/logo.svg";

/** Favicon / app-icon assets under `web/public/favicon/`. */
export const FAVICON_DIR = "/favicon";

export const BRAND_FAVICON = {
  ico: `${FAVICON_DIR}/favicon.ico`,
  svg: `${FAVICON_DIR}/favicon.svg`,
  png16: `${FAVICON_DIR}/favicon-16x16.png`,
  png32: `${FAVICON_DIR}/favicon-32x32.png`,
  png96: `${FAVICON_DIR}/favicon-96x96.png`,
  apple: `${FAVICON_DIR}/apple-touch-icon.png`,
  android192: `${FAVICON_DIR}/android-chrome-192x192.png`,
  android512: `${FAVICON_DIR}/android-chrome-512x512.png`,
  manifest: `${FAVICON_DIR}/site.webmanifest`,
} as const;

export const BRAND_LOGO_WIDTH = 838;
export const BRAND_LOGO_HEIGHT = 298;
export const BRAND_LOGO_ASPECT = BRAND_LOGO_WIDTH / BRAND_LOGO_HEIGHT;

export const BRAND_LOGO_HEIGHT_PX = {
  xs: 14,
  sm: 24,
  md: 34,
  lg: 44,
} as const;

export type BrandLogoSize = keyof typeof BRAND_LOGO_HEIGHT_PX;

/** Absolute URL for emails, JSON-LD, and other off-site references. */
export function brandLogoAbsoluteUrl(
  origin = process.env.NEXT_PUBLIC_SITE_URL?.trim() || MARKETING_ORIGIN,
): string {
  return `${origin.replace(/\/$/, "")}${BRAND_LOGO_PATH}`;
}
