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
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import {
  ShortenForm,
} from "@/components/links/shorten-form";

const router = vi.hoisted(() => ({
  replace: vi.fn(),
}));

vi.mock(
  "next/navigation",
  () => ({
    useRouter: () => router,
  }),
);

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
        "Content-Type":
          "application/json",
        ...headers,
      },
    },
  );
}

afterEach(() => {
  router.replace.mockReset();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("ShortenForm", () => {
  it("rejects unsupported URL schemes before calling the API", async () => {
    const fetchMock = vi.fn();

    vi.stubGlobal(
      "fetch",
      fetchMock,
    );

    const user = userEvent.setup();

    render(<ShortenForm />);

    await user.type(
      screen.getByLabelText(
        "Long URL",
      ),
      "ftp://example.com/file",
    );

    await user.click(
      screen.getByRole(
        "button",
        { name: "Shorten" },
      ),
    );

    expect(
      screen.getByText(
        "URL must use HTTP or HTTPS.",
      ),
    ).toBeInTheDocument();

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("renders a successful short URL and calls onCreated", async () => {
    vi.stubEnv(
      "NEXT_PUBLIC_SHORTSCALE_REDIRECT_ORIGIN",
      "https://sho.rt",
    );

    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(
          {
            short_code: "abc123",
            original_url:
              "https://example.com/path",
          },
          201,
        ),
      ),
    );

    const onCreated = vi.fn();
    const user = userEvent.setup();

    render(
      <ShortenForm
        onCreated={onCreated}
      />,
    );

    await user.type(
      screen.getByLabelText(
        "Long URL",
      ),
      "https://example.com/path",
    );

    await user.click(
      screen.getByRole(
        "button",
        { name: "Shorten" },
      ),
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

    expect(
      screen.getByText(
        "https://example.com/path",
      ),
    ).toBeInTheDocument();

    expect(onCreated).toHaveBeenCalledTimes(
      1,
    );
  });

  it("shows Retry-After feedback for rate limiting", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(
          { error: "rate limited" },
          429,
          { "Retry-After": "12" },
        ),
      ),
    );

    const user = userEvent.setup();

    render(<ShortenForm />);

    await user.type(
      screen.getByLabelText(
        "Long URL",
      ),
      "https://example.com",
    );

    await user.click(
      screen.getByRole(
        "button",
        { name: "Shorten" },
      ),
    );

    expect(
      await screen.findByText(
        "Too many links created. Try again in 12 seconds.",
      ),
    ).toBeInTheDocument();
  });
});
