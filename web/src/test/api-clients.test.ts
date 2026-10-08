import {
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import {
  APIError,
  login,
} from "@/lib/api/auth";
import {
  createShortURL,
  listOwnedURLs,
} from "@/lib/api/urls";
import {
  getRedirectAnalytics,
} from "@/lib/api/analytics";

function jsonResponse(
  body: unknown,
  status = 200,
  headers: Record<string, string> = {},
) {
  return new Response(
    JSON.stringify(body),
    {
      status,
      headers: {
        "Content-Type": "application/json",
        ...headers,
      },
    },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("frontend API clients", () => {
  it("sends login using same-origin credentials", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        id: 7,
        email: "user@example.com",
      }),
    );

    vi.stubGlobal("fetch", fetchMock);

    await login({
      email: "user@example.com",
      password: "password123",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/auth/login",
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
      }),
    );
  });

  it("preserves Retry-After for URL creation", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(
          { error: "rate limited" },
          429,
          { "Retry-After": "17" },
        ),
      ),
    );

    await expect(
      createShortURL(
        "https://example.com",
      ),
    ).rejects.toMatchObject({
      name: "APIError",
      status: 429,
      retryAfter: 17,
      message: "rate limited",
    });
  });

  it("returns owned URLs from the API envelope", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        urls: [
          {
            short_code: "abc123",
            original_url:
              "https://example.com",
            created_at:
              "2026-10-08T10:00:00Z",
            expires_at: null,
          },
        ],
      }),
    );

    vi.stubGlobal("fetch", fetchMock);

    const links =
      await listOwnedURLs(20);

    expect(links).toHaveLength(1);
    expect(links[0].short_code).toBe(
      "abc123",
    );

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/urls?limit=20",
      expect.objectContaining({
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
      }),
    );
  });

  it("encodes analytics short codes and preserves the range", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({
        short_code: "a/b",
        total_redirects: 0,
        first_redirect_at: null,
        last_redirect_at: null,
        window: {
          days: 30,
          start_at:
            "2026-09-09T00:00:00Z",
          end_at:
            "2026-10-09T00:00:00Z",
          redirects: 0,
        },
        daily: [],
      }),
    );

    vi.stubGlobal("fetch", fetchMock);

    await getRedirectAnalytics(
      "a/b",
      30,
    );

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/urls/a%2Fb/analytics?days=30",
      expect.objectContaining({
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
      }),
    );
  });

  it("normalizes network failures into APIError status zero", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockRejectedValue(
        new Error("offline"),
      ),
    );

    try {
      await createShortURL(
        "https://example.com",
      );

      throw new Error(
        "expected request to fail",
      );
    } catch (caught) {
      expect(caught).toBeInstanceOf(
        APIError,
      );

      expect(caught).toEqual(
        expect.objectContaining({
          status: 0,
          message:
            "ShortScale is temporarily unavailable. Try again.",
        }),
      );
    }
  });
});
