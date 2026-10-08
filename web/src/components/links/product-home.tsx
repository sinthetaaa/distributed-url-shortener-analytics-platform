"use client";

import { useState } from "react";

import { RecentLinks } from "./recent-links";
import { ShortenForm } from "./shorten-form";

export function ProductHome() {
  const [recentLinksVersion, setRecentLinksVersion] =
    useState(0);

  return (
    <>
      <ShortenForm
        onCreated={() =>
          setRecentLinksVersion(
            (version) => version + 1,
          )
        }
      />

      <RecentLinks
        refreshVersion={recentLinksVersion}
      />
    </>
  );
}
