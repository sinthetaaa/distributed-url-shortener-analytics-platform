"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  useCallback,
  useEffect,
  useState,
} from "react";

import { APIError } from "@/lib/api/auth";
import {
  listOwnedURLs,
  type OwnedShortURL,
} from "@/lib/api/urls";

import styles from "./recent-links.module.css";

type RecentLinksProps = {
  refreshVersion: number;
};

function publicShortURL(shortCode: string) {
  const configuredOrigin =
    process.env.NEXT_PUBLIC_SHORTSCALE_REDIRECT_ORIGIN;

  const origin = configuredOrigin
    ? configuredOrigin.replace(/\/+$/, "")
    : window.location.origin;

  return `${origin}/${shortCode}`;
}

function formatCreatedAt(value: string) {
  const date = new Date(value);

  if (Number.isNaN(date.getTime())) {
    return "Unknown date";
  }

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
  }).format(date);
}

export function RecentLinks({
  refreshVersion,
}: RecentLinksProps) {
  const router = useRouter();

  const [links, setLinks] =
    useState<OwnedShortURL[] | null>(null);

  const [error, setError] =
    useState<string | null>(null);

  const [copiedCode, setCopiedCode] =
    useState<string | null>(null);

  const loadLinks = useCallback(async () => {
    try {
      const nextLinks = await listOwnedURLs(20);

      setError(null);
      setLinks(nextLinks);
    } catch (caught) {
      if (
        caught instanceof APIError &&
        caught.status === 401
      ) {
        router.replace(
          "/auth/login?reason=expired",
        );
        return;
      }

      if (
        caught instanceof APIError &&
        caught.status === 0
      ) {
        setError(
          "Recent links are temporarily unavailable.",
        );
        return;
      }

      setError(
        "Couldn’t load your recent links.",
      );
    }
  }, [router]);

  useEffect(() => {
    let active = true;

    void listOwnedURLs(20).then(
      (nextLinks) => {
        if (!active) {
          return;
        }

        setError(null);
        setLinks(nextLinks);
      },
      (caught: unknown) => {
        if (!active) {
          return;
        }

        if (
          caught instanceof APIError &&
          caught.status === 401
        ) {
          router.replace(
            "/auth/login?reason=expired",
          );
          return;
        }

        if (
          caught instanceof APIError &&
          caught.status === 0
        ) {
          setError(
            "Recent links are temporarily unavailable.",
          );
          return;
        }

        setError(
          "Couldn’t load your recent links.",
        );
      },
    );

    return () => {
      active = false;
    };
  }, [router, refreshVersion]);

  async function handleCopy(
    shortCode: string,
  ) {
    try {
      await navigator.clipboard.writeText(
        publicShortURL(shortCode),
      );

      setCopiedCode(shortCode);

      window.setTimeout(() => {
        setCopiedCode((current) =>
          current === shortCode
            ? null
            : current,
        );
      }, 1800);
    } catch {
      setError(
        "Couldn’t copy the short link.",
      );
    }
  }

  return (
    <section
      className={styles.section}
      id="recent-links"
      aria-labelledby="recent-links-title"
    >
      <div className={styles.headingRow}>
        <div>
          <h2
            className={styles.heading}
            id="recent-links-title"
          >
            Recent links
          </h2>

          <p className={styles.subheading}>
            Your newest shortened URLs.
          </p>
        </div>

        {error ? (
          <button
            className={styles.retry}
            type="button"
            onClick={() => void loadLinks()}
          >
            Retry
          </button>
        ) : null}
      </div>

      {error ? (
        <p
          className={styles.error}
          role="alert"
        >
          {error}
        </p>
      ) : null}

      {links === null && !error ? (
        <div
          className={styles.skeletonList}
          aria-label="Loading recent links"
        >
          <div className={styles.skeletonRow} />
          <div className={styles.skeletonRow} />
          <div className={styles.skeletonRow} />
        </div>
      ) : null}

      {links && links.length === 0 ? (
        <div className={styles.empty}>
          <p>No links yet.</p>
          <p>
            Your new short links will appear here.
          </p>
        </div>
      ) : null}

      {links && links.length > 0 ? (
        <ul className={styles.list}>
          {links.map((link) => {
            const shortURL = publicShortURL(
              link.short_code,
            );

            return (
              <li
                className={styles.row}
                key={link.short_code}
              >
                <div className={styles.primary}>
                  <a
                    className={styles.shortURL}
                    href={shortURL}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {shortURL}
                  </a>

                  <p className={styles.destination}>
                    {link.original_url}
                  </p>
                </div>

                <div className={styles.meta}>
                  <time
                    dateTime={link.created_at}
                  >
                    {formatCreatedAt(
                      link.created_at,
                    )}
                  </time>
                </div>

                <div className={styles.actions}>
                  <Link
                    className={styles.actionLink}
                    href={`/links/${encodeURIComponent(
                      link.short_code,
                    )}/analytics`}
                  >
                    Analytics
                  </Link>

                  <button
                    className={styles.actionButton}
                    type="button"
                    onClick={() =>
                      void handleCopy(
                        link.short_code,
                      )
                    }
                  >
                    {copiedCode ===
                    link.short_code
                      ? "Copied"
                      : "Copy"}
                  </button>
                </div>
              </li>
            );
          })}
        </ul>
      ) : null}
    </section>
  );
}
