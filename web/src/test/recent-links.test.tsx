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
  RecentLinks,
} from "@/components/links/recent-links";

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

afterEach(() => {
  router.replace.mockReset();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("RecentLinks", () => {
  it("renders the empty state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({
          urls: [],
        }),
      ),
    );

    render(
      <RecentLinks
        refreshVersion={0}
      />,
    );

    expect(
      await screen.findByText(
        "No links yet.",
      ),
    ).toBeInTheDocument();

    expect(
      screen.getByText(
        "Your new short links will appear here.",
      ),
    ).toBeInTheDocument();
  });

  it("renders a link and announces clipboard success", async () => {
    vi.stubEnv(
      "NEXT_PUBLIC_SHORTSCALE_REDIRECT_ORIGIN",
      "https://sho.rt",
    );

    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({
          urls: [
            {
              short_code: "abc123",
              original_url:
                "https://example.com/path",
              created_at:
                "2026-10-08T10:00:00Z",
              expires_at: null,
            },
          ],
        }),
      ),
    );

    const writeText = vi
      .fn()
      .mockResolvedValue(undefined);

    const user = userEvent.setup();

    Object.defineProperty(
      navigator,
      "clipboard",
      {
        configurable: true,
        value: {
          writeText,
        },
      },
    );

    render(
      <RecentLinks
        refreshVersion={0}
      />,
    );

    expect(
      await screen.findByRole(
        "link",
        {
          name:
            "https://sho.rt/abc123",
        },
      ),
    ).toBeInTheDocument();

    await user.click(
      screen.getByRole(
        "button",
        { name: "Copy" },
      ),
    );

    expect(writeText).toHaveBeenCalledWith(
      "https://sho.rt/abc123",
    );

    expect(
      await screen.findByRole(
        "button",
        { name: "Copied" },
      ),
    ).toBeInTheDocument();

    expect(
      screen.getByText(
        "Short link abc123 copied to clipboard.",
      ),
    ).toBeInTheDocument();
  });

  it("shows clipboard failure without replacing the list state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({
          urls: [
            {
              short_code: "abc123",
              original_url:
                "https://example.com/path",
              created_at:
                "2026-10-08T10:00:00Z",
              expires_at: null,
            },
          ],
        }),
      ),
    );

    const user = userEvent.setup();

    Object.defineProperty(
      navigator,
      "clipboard",
      {
        configurable: true,
        value: {
          writeText: vi
            .fn()
            .mockRejectedValue(
              new Error(
                "clipboard denied",
              ),
            ),
        },
      },
    );

    render(
      <RecentLinks
        refreshVersion={0}
      />,
    );

    await screen.findByRole(
      "button",
      { name: "Copy" },
    );

    await user.click(
      screen.getByRole(
        "button",
        { name: "Copy" },
      ),
    );

    expect(
      await screen.findByText(
        /Copy failed\./,
      ),
    ).toBeInTheDocument();

    expect(
      screen.queryByRole(
        "button",
        { name: "Retry" },
      ),
    ).not.toBeInTheDocument();
  });

  it("redirects when the session has expired", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(
          {
            error:
              "authentication required",
          },
          401,
        ),
      ),
    );

    render(
      <RecentLinks
        refreshVersion={0}
      />,
    );

    await waitFor(() => {
      expect(
        router.replace,
      ).toHaveBeenCalledWith(
        "/auth/login?reason=expired",
      );
    });
  });
});
