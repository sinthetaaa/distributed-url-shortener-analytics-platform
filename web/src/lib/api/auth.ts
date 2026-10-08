export type AuthUser = {
  id: number;
  email: string;
};

type AuthPayload = {
  email: string;
  password: string;
};

type APIErrorBody = {
  error?: string;
};

export class APIError extends Error {
  status: number;
  retryAfter: number | null;

  constructor(
    message: string,
    status: number,
    retryAfter: number | null = null,
  ) {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.retryAfter = retryAfter;
  }
}

async function parseError(response: Response): Promise<never> {
  let message = "Request failed.";

  try {
    const body = (await response.json()) as APIErrorBody;

    if (body.error) {
      message = body.error;
    }
  } catch {
    // Keep the stable fallback for non-JSON failure responses.
  }

  const retryAfterHeader = response.headers.get("Retry-After");
  const parsedRetryAfter = retryAfterHeader
    ? Number.parseInt(retryAfterHeader, 10)
    : Number.NaN;

  const retryAfter =
    Number.isFinite(parsedRetryAfter) && parsedRetryAfter > 0
      ? parsedRetryAfter
      : null;

  throw new APIError(message, response.status, retryAfter);
}

async function authRequest(
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
    return parseError(response);
  }

  return response;
}

export async function register(
  payload: AuthPayload,
): Promise<AuthUser> {
  const response = await authRequest("/api/v1/auth/register", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });

  return (await response.json()) as AuthUser;
}

export async function login(
  payload: AuthPayload,
): Promise<AuthUser> {
  const response = await authRequest("/api/v1/auth/login", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
  });

  return (await response.json()) as AuthUser;
}

export async function getCurrentUser(): Promise<AuthUser> {
  const response = await authRequest("/api/v1/auth/me", {
    method: "GET",
    cache: "no-store",
  });

  return (await response.json()) as AuthUser;
}

export async function logout(): Promise<void> {
  await authRequest("/api/v1/auth/logout", {
    method: "POST",
  });
}
