import { fact0BackendUrl } from "./fact0-env";
/** Stream responses, including SSE, without buffering. Credentials never enter client code. */
export async function proxyBackend(req: Request, path: string) {
  const headers = new Headers();
  for (const key of [
    "authorization",
    "content-type",
    "accept",
    "x-fact0-sync",
  ]) {
    const value = req.headers.get(key);
    if (value) headers.set(key, value);
  }
  const target = new URL(path + new URL(req.url).search, fact0BackendUrl());
  const body =
    req.method === "GET" || req.method === "HEAD"
      ? undefined
      : await req.arrayBuffer();
  try {
    const response = await fetch(target, {
      method: req.method,
      headers,
      body,
      cache: "no-store",
      redirect: "manual",
      signal: req.signal,
    });
    const outgoing = new Headers({ "Cache-Control": "no-store" });
    for (const key of [
      "content-type",
      "content-disposition",
      "retry-after",
      "x-accel-buffering",
    ]) {
      const value = response.headers.get(key);
      if (value) outgoing.set(key, value);
    }
    return new Response(
      req.method === "HEAD" || response.status === 204 ? null : response.body,
      { status: response.status, headers: outgoing },
    );
  } catch {
    return Response.json(
      { error: "The local Fact0 API is unavailable." },
      { status: 502 },
    );
  }
}
