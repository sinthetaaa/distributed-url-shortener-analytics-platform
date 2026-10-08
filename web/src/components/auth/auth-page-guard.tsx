"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import {
  APIError,
  getCurrentUser,
} from "@/lib/api/auth";

type AuthPageGuardProps = {
  children: React.ReactNode;
};

export function AuthPageGuard({
  children,
}: AuthPageGuardProps) {
  const router = useRouter();
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let active = true;

    async function checkSession() {
      try {
        await getCurrentUser();

        if (active) {
          router.replace("/");
        }
      } catch (error) {
        if (!active) {
          return;
        }

        if (error instanceof APIError && error.status === 401) {
          setReady(true);
          return;
        }

        // Auth pages remain usable if the session check itself
        // cannot reach the backend. Form submission will surface
        // the actual connectivity failure.
        setReady(true);
      }
    }

    void checkSession();

    return () => {
      active = false;
    };
  }, [router]);

  if (!ready) {
    return null;
  }

  return children;
}
