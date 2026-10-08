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

function listErrorMessage(caught: unknown) {
  if (
    caught instanceof APIError &&
    caught.status === 0
  ) {
    return "Recent links are temporarily unavailable.";
  }

  return "Couldn’t load your recent links.";
}

export function RecentLinks({
  refreshVersion,
}: RecentLinksProps) {
  const router = useRouter();

  const [links, setLinks] =
    useState<OwnedShortURL[] | null>(null);

  const [loadError, setLoadError] =
    useState<string | null>(null);

  const [retrying, setRetrying] =
    useState(false);

  const [copiedCode, setCopiedCode] =
    useState<string | null>(null);

  const [copyErrorCode, setCopyErrorCode] =
    useState<string | null>(null);

  const loadLinks = useCallback(async () => {
    if (retrying) {
      return;
    }

    setRetrying(true);

    try {
      const nextLinks =
        await listOwnedURLs(20);

      setLoadError(null);
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

      setLoadError(
        listErrorMessage(caught),
      );
    } finally {
      setRetrying(false);
    }
  }, [retrying, router]);

  useEffect(() => {
    let active = true;

    void listOwnedURLs(20).then(
      (nextLinks) => {
        if (!active) {
          return;
        }

        setLoadError(null);
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

        setLoadError(
          listErrorMessage(caught),
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
    setCopyErrorCode(null);

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
      setCopiedCode(null);
      setCopyErrorCode(shortCode);
    }
  }

  return (
    <section
      className={styles.section}
      id="recent-links"
      aria-labelledby="recent-links-title"
      aria-busy={retrying}
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

        {loadError ? (
          <button
            className={styles.retry}
            type="button"
            disabled={retrying}
            onClick={() =>
              void loadLinks()
            }
          >
            {retrying
              ? "Retrying…"
              : "Retry"}
          </button>
        ) : null}
      </div>

      {loadError ? (
        <p
          className={styles.error}
          role="alert"
        >
          {loadError}
        </p>
      ) : null}

      {links === null && !loadError ? (
        <div
          className={styles.skeletonList}
          role="status"
          aria-live="polite"
        >
          <span className="sr-only">
            Loading recent links.
          </span>

          <div
            className={styles.skeletonRow}
            aria-hidden="true"
          />

          <div
            className={styles.skeletonRow}
            aria-hidden="true"
          />

          <div
            className={styles.skeletonRow}
            aria-hidden="true"
          />
        </div>
      ) : null}

      {links &&
      links.length === 0 &&
      !loadError ? (
        <div className={styles.empty}>
          <p>No links yet.</p>

          <p>
            Your new short links will appear here.
          </p>
        </div>
      ) : null}

      <div
        className="sr-only"
        role="status"
        aria-live="polite"
        aria-atomic="true"
      >
        {copiedCode
          ? `Short link ${copiedCode} copied to clipboard.`
          : ""}
      </div>

      {links && links.length > 0 ? (
        <ul className={styles.list}>
          {links.map((link) => {
            const shortURL = publicShortURL(
              link.short_code,
            );

            const copyFailed =
              copyErrorCode ===
              link.short_code;

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

                <div className={styles.actionArea}>
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

                  {copyFailed ? (
                    <p
                      className={styles.actionError}
                      role="alert"
                    >
                      Copy failed. Select the
                      short URL and copy it
                      manually.
                    </p>
                  ) : null}
                </div>
              </li>
            );
          })}
        </ul>
      ) : null}
    </section>
  );
}
