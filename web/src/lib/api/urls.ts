import { APIError } from "@/lib/api/auth";

export type CreatedShortURL = {
  short_code: string;
  original_url: string;
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

export async function createShortURL(
  originalURL: string,
): Promise<CreatedShortURL> {
  let response: Response;

  try {
    response = await fetch("/api/v1/urls", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        url: originalURL,
      }),
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

  return (await response.json()) as CreatedShortURL;
}
