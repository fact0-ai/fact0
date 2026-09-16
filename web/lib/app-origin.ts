/** All dashboard links stay on the operator's configured origin. */
export const MARKETING_ORIGIN =
  process.env.NEXT_PUBLIC_SITE_URL?.trim() || "http://localhost:3000";
export const APP_ORIGIN = MARKETING_ORIGIN;
export const APP_HOST = new URL(APP_ORIGIN).host;
export function resolveMetadataBase() {
  return MARKETING_ORIGIN;
}
export function hostOnly(host: string) {
  return host.split(":")[0]?.toLowerCase() ?? "";
}
export function isMarketingHost() {
  return false;
}
export function isAppHost() {
  return true;
}
export function isUnifiedHost() {
  return true;
}
export function usesSubdomainSplit() {
  return false;
}
export const APP_SURFACE_PREFIXES = ["/dashboard", "/executions"] as const;
export function isAppSurfacePath(path: string) {
  return APP_SURFACE_PREFIXES.some(
    (p) => path === p || path.startsWith(p + "/"),
  );
}
export const isProtectedPath = isAppSurfacePath;
export function sanitiseRedirectPath(
  raw: string | null | undefined,
  fallback = "/dashboard",
) {
  if (
    !raw ||
    !raw.startsWith("/") ||
    raw.startsWith("//") ||
    /[\\\u0000-\u001f]/.test(raw)
  )
    return fallback;
  const url = new URL(raw, "http://fact0.invalid");
  if (url.origin !== "http://fact0.invalid" || !isAppSurfacePath(url.pathname))
    return fallback;
  return url.pathname + url.search + url.hash;
}
export function appHref(path: string) {
  return path.startsWith("/") ? path : `/${path}`;
}
export const marketingHref = appHref;
export const resolvePostAuthDestination = sanitiseRedirectPath;
export function signInHref(redirectPath?: string) {
  return (
    "/sign-in" +
    (redirectPath
      ? `?redirect_url=${encodeURIComponent(sanitiseRedirectPath(redirectPath))}`
      : "")
  );
}
