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
  AuthForm,
} from "@/components/auth/auth-form";

const router = vi.hoisted(() => ({
  push: vi.fn(),
  replace: vi.fn(),
  refresh: vi.fn(),
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
  router.push.mockReset();
  router.replace.mockReset();
  router.refresh.mockReset();
  vi.unstubAllGlobals();
});

describe("AuthForm", () => {
  it("validates required fields without calling the API", async () => {
    const fetchMock = vi.fn();

    vi.stubGlobal(
      "fetch",
      fetchMock,
    );

    const user = userEvent.setup();

    render(
      <AuthForm mode="login" />,
    );

    await user.click(
      screen.getByRole(
        "button",
        { name: "Sign in" },
      ),
    );

    expect(
      screen.getByText(
        "Enter your email.",
      ),
    ).toBeInTheDocument();

    expect(
      screen.getByText(
        "Enter your password.",
      ),
    ).toBeInTheDocument();

    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("uses a generic invalid-credentials error", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse(
          {
            error:
              "specific backend detail",
          },
          401,
        ),
      ),
    );

    const user = userEvent.setup();

    render(
      <AuthForm mode="login" />,
    );

    await user.type(
      screen.getByLabelText("Email"),
      "user@example.com",
    );

    await user.type(
      screen.getByLabelText("Password"),
      "password123",
    );

    await user.click(
      screen.getByRole(
        "button",
        { name: "Sign in" },
      ),
    );

    expect(
      await screen.findByText(
        "Invalid email or password.",
      ),
    ).toBeInTheDocument();

    expect(
      router.replace,
    ).not.toHaveBeenCalled();
  });

  it("validates registration confirmation locally", async () => {
    const fetchMock = vi.fn();

    vi.stubGlobal(
      "fetch",
      fetchMock,
    );

    const user = userEvent.setup();

    render(
      <AuthForm mode="register" />,
    );

    await user.type(
      screen.getByLabelText("Email"),
      "user@example.com",
    );

    await user.type(
      screen.getByLabelText("Password"),
      "password123",
    );

    await user.type(
      screen.getByLabelText(
        "Confirm password",
      ),
      "different123",
    );

    await user.click(
      screen.getByRole(
        "button",
        {
          name: "Create account",
        },
      ),
    );

    expect(
      screen.getByText(
        "Passwords don’t match.",
      ),
    ).toBeInTheDocument();

    expect(fetchMock).not.toHaveBeenCalled();
  });
});
