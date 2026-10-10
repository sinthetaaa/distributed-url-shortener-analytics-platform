import type { NextRequest } from "next/server";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const apiOrigin =
  process.env.SHORTSCALE_API_ORIGIN ??
  "http://localhost:18080";

type RouteContext = {
  params: Promise<{
    path: string[];
  }>;
};

async function proxyRequest(
  request: NextRequest,
  context: RouteContext,
): Promise<Response> {
  const { path } = await context.params;

  const encodedPath = path
    .map((segment) => encodeURIComponent(segment))
    .join("/");

  const target = new URL(
    `${apiOrigin.replace(/\/$/, "")}/api/${encodedPath}${request.nextUrl.search}`,
  );

  const requestHeaders = new Headers(request.headers);

  requestHeaders.delete("host");
  requestHeaders.delete("content-length");
  requestHeaders.delete("connection");

  const init: RequestInit = {
    method: request.method,
    headers: requestHeaders,
    redirect: "manual",
    cache: "no-store",
  };

  if (
    request.method !== "GET" &&
    request.method !== "HEAD"
  ) {
    init.body = await request.arrayBuffer();
  }

  let upstream: Response;

  try {
    upstream = await fetch(target, init);
  } catch (error) {
    console.error(
      "ShortScale API proxy upstream request failed",
      error,
    );

    return Response.json(
      {
        error:
          "ShortScale is temporarily unavailable. Try again.",
      },
      {
        status: 502,
      },
    );
  }

  const responseHeaders = new Headers(
    upstream.headers,
  );

  responseHeaders.delete("connection");
  responseHeaders.delete("keep-alive");
  responseHeaders.delete("proxy-authenticate");
  responseHeaders.delete("proxy-authorization");
  responseHeaders.delete("te");
  responseHeaders.delete("trailer");
  responseHeaders.delete("transfer-encoding");
  responseHeaders.delete("upgrade");
  responseHeaders.delete("content-length");
  responseHeaders.delete("content-encoding");

  responseHeaders.set("Cache-Control", "no-store");

  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: responseHeaders,
  });
}

export const GET = proxyRequest;
export const POST = proxyRequest;
export const PUT = proxyRequest;
export const PATCH = proxyRequest;
export const DELETE = proxyRequest;
export const OPTIONS = proxyRequest;
