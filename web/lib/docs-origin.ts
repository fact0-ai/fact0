/** Mintlify docs - hosted at docs.fact0.io (not proxied through fact0.io). */

export const DOCS_ORIGIN =
  process.env.NEXT_PUBLIC_DOCS_URL?.trim() ||
  process.env.FACT0_MINTLIFY_URL?.trim() ||
  "https://docs.fact0.io";

/** Absolute URL on the docs site. Omit path for the docs home. */
export function docsHref(path?: string): string {
  if (!path) return DOCS_ORIGIN;
  const raw = path.startsWith("/docs")
    ? path.slice("/docs".length) || "/introduction"
    : path.startsWith("/")
      ? path
      : `/${path}`;
  return `${DOCS_ORIGIN}${raw}`;
}
