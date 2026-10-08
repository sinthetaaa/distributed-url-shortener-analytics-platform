import { APIError } from "@/lib/api/auth";

export type CreatedShortURL = {
  short_code: string;
  original_url: string;
};

export type OwnedShortURL = {
  short_code: string;
  original_url: string;
  created_at: string;
  expires_at: string | null;
};

type ListOwnedURLsResponse = {
  urls: OwnedShortURL[];
};

type APIErrorBody = {
  error?: string;
};

async function parseURLAPIError(
  response: Response,
): Promise<never> {
  let message = "Request failed.";

  try {
    const body = (await response.json()) as APIErrorBody;

    if (body.error) {
      message = body.error;
    }
  } catch {
    // Keep the stable fallback when the backend does not return JSON.
  }

  const retryAfterHeader = response.headers.get("Retry-After");
  const parsedRetryAfter = retryAfterHeader
    ? Number.parseInt(retryAfterHeader, 10)
    : Number.NaN;

  const retryAfter =
    Number.isFinite(parsedRetryAfter) && parsedRetryAfter > 0
      ? parsedRetryAfter
      : null;

  throw new APIError(
    message,
    response.status,
    retryAfter,
  );
}

async function urlRequest(
  path: string,
  init?: RequestInit,
): Promise<Response> {
  let response: Response;

  try {
    response = await fetch(path, {
      ...init,
      credentials: "same-origin",
      headers: {
        ...init?.headers,
        Accept: "application/json",
      },
    });
  } catch {
    throw new APIError(
      "ShortScale is temporarily unavailable. Try again.",
      0,
    );
  }

  if (!response.ok) {
    return parseURLAPIError(response);
  }

  return response;
}

export async function createShortURL(
  originalURL: string,
): Promise<CreatedShortURL> {
  const response = await urlRequest("/api/v1/urls", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      url: originalURL,
    }),
  });

  return (await response.json()) as CreatedShortURL;
}

export async function listOwnedURLs(
  limit = 20,
): Promise<OwnedShortURL[]> {
  const response = await urlRequest(
    `/api/v1/urls?limit=${limit}`,
    {
      method: "GET",
      cache: "no-store",
    },
  );

  const body =
    (await response.json()) as ListOwnedURLsResponse;

  return body.urls;
}
