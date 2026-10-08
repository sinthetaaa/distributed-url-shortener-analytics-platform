import type {
  ReactNode,
} from "react";

import {
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import {
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import {
  AnalyticsPage,
} from "@/components/analytics/analytics-page";

const router = vi.hoisted(() => ({
  replace: vi.fn(),
}));

vi.mock(
  "next/navigation",
  () => ({
    useRouter: () => router,
  }),
);

vi.mock(
  "next/link",
  () => ({
    default: ({
      href,
      children,
    }: {
      href: string;
      children: ReactNode;
    }) => (
      <a href={href}>
        {children}
      </a>
    ),
  }),
);

vi.mock(
  "@/components/auth/logout-button",
  () => ({
    LogoutButton: () => (
      <button type="button">
        Log out
      </button>
    ),
  }),
);

function jsonResponse(
  body: unknown,
  status = 200,
) {
  return new Response(
    JSON.stringify(body),
    {
      status,
      headers: {
        "Content-Type":
          "application/json",
      },
    },
  );
}

function makeAnalytics(
  days: number,
  redirects: number,
) {
  const daily = Array.from(
    { length: days },
    (_, index) => {
      const date = new Date(
        Date.UTC(
          2026,
          9,
          8 - (days - 1 - index),
        ),
      );

      return {
        date: date
          .toISOString()
          .slice(0, 10),
        redirects:
          index === days - 1
            ? redirects
            : 0,
      };
    },
  );

  return {
    short_code: "abc123",
    total_redirects: redirects,
    first_redirect_at:
      redirects > 0
        ? "2026-10-08T10:00:00Z"
        : null,
    last_redirect_at:
      redirects > 0
        ? "2026-10-08T11:00:00Z"
        : null,
    window: {
      days,
      start_at:
        `${daily[0].date}T00:00:00Z`,
      end_at:
        "2026-10-09T00:00:00Z",
      redirects,
    },
    daily,
  };
}

afterEach(() => {
  router.replace.mockReset();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("AnalyticsPage", () => {
  it("renders the neutral 404 state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(
          {
            error:
              "short URL not found",
          },
          404,
        ),
      ),
    );

    render(
      <AnalyticsPage
        shortCode="missing"
      />,
    );

    expect(
      await screen.findByText(
        "Analytics not found.",
      ),
    ).toBeInTheDocument();

    expect(
      screen.getByText(
        /does not exist or is not available/,
      ),
    ).toBeInTheDocument();
  });

  it("renders the zero state without a chart", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(
          makeAnalytics(
            7,
            0,
          ),
        ),
      ),
    );

    render(
      <AnalyticsPage
        shortCode="abc123"
      />,
    );

    expect(
      await screen.findByText(
        "No redirects in this period.",
      ),
    ).toBeInTheDocument();

    expect(
      screen.queryByRole("img"),
    ).not.toBeInTheDocument();
  });

  it("renders the chart text equivalent and switches range", async () => {
    const fetchMock = vi
      .fn()
      .mockImplementation(
        (input: RequestInfo | URL) => {
          const url = String(input);

          const days =
            url.includes(
              "days=30",
            )
              ? 30
              : 7;

          return Promise.resolve(
            jsonResponse(
              makeAnalytics(
                days,
                3,
              ),
            ),
          );
        },
      );

    vi.stubGlobal(
      "fetch",
      fetchMock,
    );

    const user = userEvent.setup();

    render(
      <AnalyticsPage
        shortCode="abc123"
      />,
    );

    expect(
      await screen.findByRole(
        "table",
        {
          name:
            "Daily redirect counts for the last 7 days",
        },
      ),
    ).toBeInTheDocument();

    expect(
      screen.getByRole("img"),
    ).toBeInTheDocument();

    const range30 =
      screen.getByRole(
        "button",
        { name: "30d" },
      );

    await user.click(range30);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/v1/urls/abc123/analytics?days=30",
        expect.objectContaining({
          credentials:
            "same-origin",
        }),
      );
    });

    await waitFor(() => {
      expect(
        screen.getByRole(
          "button",
          { name: "30d" },
        ),
      ).toHaveAttribute(
        "aria-pressed",
        "true",
      );
    });

    expect(
      await screen.findByRole(
        "table",
        {
          name:
            "Daily redirect counts for the last 30 days",
        },
      ),
    ).toBeInTheDocument();
  });
});
