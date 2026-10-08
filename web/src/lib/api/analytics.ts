import { APIError } from "@/lib/api/auth";

export type DailyRedirectCount = {
  date: string;
  redirects: number;
};

export type RedirectAnalytics = {
  short_code: string;
  total_redirects: number;
  first_redirect_at: string | null;
  last_redirect_at: string | null;
  window: {
    days: number;
    start_at: string;
    end_at: string;
    redirects: number;
  };
  daily: DailyRedirectCount[];
};

type APIErrorBody = {
  error?: string;
};

async function parseAnalyticsError(
  response: Response,
): Promise<never> {
  let message = "Request failed.";

  try {
    const body =
      (await response.json()) as APIErrorBody;

    if (body.error) {
      message = body.error;
    }
  } catch {
    // Keep the stable fallback.
  }

  throw new APIError(
    message,
    response.status,
  );
}

export async function getRedirectAnalytics(
  shortCode: string,
  days: number,
): Promise<RedirectAnalytics> {
  let response: Response;

  try {
    response = await fetch(
      `/api/v1/urls/${encodeURIComponent(
        shortCode,
      )}/analytics?days=${days}`,
      {
        method: "GET",
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
        },
        cache: "no-store",
      },
    );
  } catch {
    throw new APIError(
      "ShortScale is temporarily unavailable. Try again.",
      0,
    );
  }

  if (!response.ok) {
    return parseAnalyticsError(response);
  }

  return (await response.json()) as RedirectAnalytics;
}
