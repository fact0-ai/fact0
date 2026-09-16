/** Set at build time only when the published docs match this release. */
export const DOCS_ORIGIN =
  process.env.NEXT_PUBLIC_DOCS_URL?.trim().replace(/\/+$/, "") || "";

const SOURCE_ORIGIN = "https://github.com/fact0-ai/fact0/blob/main";

/** Default to the release instructions until a published docs URL is supplied. */
export function docsHref(path?: string): string {
  const raw = (path || "")
    .replace(/^\/docs(?:\/|$)/, "")
    .replace(/^\/+/, "");
  if (DOCS_ORIGIN) return raw ? `${DOCS_ORIGIN}/${raw}` : DOCS_ORIGIN;

  const [pathname, fragment] = raw.split("#", 2);
  const page = pathname.replace(/\/+$/, "");
  if (!fragment) {
    if (!page || page === "quickstart")
      return `${SOURCE_ORIGIN}/README.md#quickstart`;
    if (page === "sdk/python/installation")
      return `${SOURCE_ORIGIN}/README.md#python`;
  }
  return `${SOURCE_ORIGIN}/docs/${page || "introduction"}.mdx${fragment ? `#${fragment}` : ""}`;
}
