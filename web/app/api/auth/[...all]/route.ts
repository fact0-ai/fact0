import { getAuth } from "@/lib/auth";

export const runtime = "nodejs";
const reads = new Set([
  "get-session",
  "token",
  "jwks",
  "organization/get-full-organization",
  "organization/list",
  "organization/get-active-member",
]);
const writes = new Set(["sign-in/email", "sign-out"]);
function handle(req: Request) {
  const path = new URL(req.url).pathname
    .replace(/^\/api\/auth\//, "")
    .replace(/\/$/, "");
  if (!(req.method === "GET" ? reads : writes).has(path)) {
    return Response.json(
      { error: "This endpoint is not available in the single-owner edition." },
      { status: 404 },
    );
  }
  return getAuth().handler(req);
}
export const GET = handle;
export const POST = handle;
