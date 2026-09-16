import { proxyBackend } from "@/lib/backend-proxy";
export const runtime = "nodejs";
async function handle(
  req: Request,
  context: { params: Promise<{ path: string[] }> },
) {
  const { path } = await context.params;
  return proxyBackend(req, "/api/v1/" + path.map(encodeURIComponent).join("/"));
}
export const GET = handle;
export const POST = handle;
export const PUT = handle;
export const DELETE = handle;
export const HEAD = handle;
