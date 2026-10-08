"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import {
  APIError,
  logout,
} from "@/lib/api/auth";

export function LogoutButton() {
  const router = useRouter();

  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleLogout() {
    setPending(true);
    setError(null);

    try {
      await logout();

      router.replace("/auth/login");
      router.refresh();
    } catch (caught) {
      if (
        caught instanceof APIError &&
        caught.status === 0
      ) {
        setError(
          "Couldn’t log out. ShortScale is temporarily unavailable.",
        );
      } else {
        setError("Couldn’t log out. Try again.");
      }

      setPending(false);
    }
  }

  return (
    <div className="foundation__logout">
      <button
        className="foundation__logout-button"
        type="button"
        disabled={pending}
        onClick={handleLogout}
      >
        {pending ? "Logging out…" : "Log out"}
      </button>

      {error ? (
        <p className="foundation__logout-error" role="alert">
          {error}
        </p>
      ) : null}
    </div>
  );
}
