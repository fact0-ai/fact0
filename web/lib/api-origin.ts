/** Browser requests always use the local Next.js proxy. */
export function apiOriginTrimmed() { return ""; }
export function browserApiURL(path: string) { return path.startsWith("/") ? path : `/${path}`; }
