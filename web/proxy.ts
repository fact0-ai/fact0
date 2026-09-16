import { getSessionCookie } from "better-auth/cookies";
import { NextResponse, type NextRequest } from "next/server";
import { isProtectedPath } from "@/lib/app-origin";

export function proxy(req: NextRequest) {
  // Reserve the removed fixed route so [sessionId] cannot render it as a session.
  let pathname = req.nextUrl.pathname;
  try {
    pathname = decodeURIComponent(pathname);
  } catch {
    /* Keep malformed paths unchanged. */
  }
  const retired = "/dashboard/coding-agents/governance";
  if (pathname === retired || pathname.startsWith(retired + "/")) {
    return new NextResponse("Not found", { status: 404 });
  }

  // Cookie presence is only a fast redirect hint; the server layout validates it.
  if (isProtectedPath(req.nextUrl.pathname) && !getSessionCookie(req)) {
    const url = new URL("/sign-in", req.url);
    url.searchParams.set(
      "redirect_url",
      req.nextUrl.pathname + req.nextUrl.search,
    );
    return NextResponse.redirect(url);
  }
  return NextResponse.next();
}
export const config = { matcher: ["/dashboard/:path*", "/executions/:path*"] };
