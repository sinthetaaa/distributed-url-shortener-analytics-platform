export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const apiOrigin =
  process.env.SHORTSCALE_API_ORIGIN ??
  "http:" + "//localhost:18080";

type RouteContext = {
  params: Promise<{
    shortCode: string;
  }>;
};

export async function GET(
  request: Request,
  context: RouteContext,
) {
  const { shortCode } = await context.params;

  const upstream = await fetch(
    apiOrigin + "/" + encodeURIComponent(shortCode),
    {
      method: "GET",
      headers: {
        "user-agent":
          request.headers.get("user-agent") ??
          "ShortScale-Vercel-Redirect",
      },
      redirect: "manual",
      cache: "no-store",
    },
  );

  const location = upstream.headers.get("location");

  if (
    location &&
    upstream.status >= 300 &&
    upstream.status < 400
  ) {
    return new Response(null, {
      status: upstream.status,
      headers: {
        location,
        "cache-control": "no-store",
      },
    });
  }

  const body = await upstream.arrayBuffer();
  const headers = new Headers();

  const contentType = upstream.headers.get("content-type");

  if (contentType) {
    headers.set("content-type", contentType);
  }

  headers.set("cache-control", "no-store");

  return new Response(body, {
    status: upstream.status,
    headers,
  });
}
