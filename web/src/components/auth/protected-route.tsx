"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

import {
  APIError,
  getCurrentUser,
} from "@/lib/api/auth";

type ProtectedRouteProps = {
  children: React.ReactNode;
};

export function ProtectedRoute({
  children,
}: ProtectedRouteProps) {
  const router = useRouter();
  const [authorized, setAuthorized] = useState(false);

  useEffect(() => {
    let active = true;

    async function checkSession() {
      try {
        await getCurrentUser();

        if (active) {
          setAuthorized(true);
        }
      } catch (error) {
        if (!active) {
          return;
        }

        if (error instanceof APIError && error.status === 401) {
          router.replace("/auth/login");
          return;
        }

        router.replace("/auth/login?reason=unavailable");
      }
    }

    void checkSession();

    return () => {
      active = false;
    };
  }, [router]);

  if (!authorized) {
    return null;
  }

  return children;
}
