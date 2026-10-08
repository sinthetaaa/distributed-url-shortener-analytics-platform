"use client";

import {
  FormEvent,
  useMemo,
  useState,
} from "react";
import { useRouter } from "next/navigation";

import { APIError } from "@/lib/api/auth";
import {
  createShortURL,
  type CreatedShortURL,
} from "@/lib/api/urls";

import styles from "./shorten-form.module.css";

function validateURL(value: string) {
  if (!value) {
    return "Paste a URL to shorten.";
  }

  let parsed: URL;

  try {
    parsed = new URL(value);
  } catch {
    return "Enter a valid HTTP or HTTPS URL.";
  }

  if (
    parsed.protocol !== "http:" &&
    parsed.protocol !== "https:"
  ) {
    return "URL must use HTTP or HTTPS.";
  }

  return null;
}

function getRateLimitMessage(
  retryAfter: number | null,
) {
  if (retryAfter) {
    return `Too many links created. Try again in ${retryAfter} seconds.`;
  }

  return "Too many links created. Try again shortly.";
}

type ShortenFormProps = {
  onCreated?: () => void;
};

export function ShortenForm({
  onCreated,
}: ShortenFormProps) {
  const router = useRouter();

  const [url, setURL] = useState("");
  const [fieldError, setFieldError] =
    useState<string | null>(null);
  const [formError, setFormError] =
    useState<string | null>(null);

  const [result, setResult] =
    useState<CreatedShortURL | null>(null);

  const [pending, setPending] = useState(false);
  const [copyState, setCopyState] = useState<
    "idle" | "copied" | "error"
  >("idle");

  const shortURL = useMemo(() => {
    if (!result) {
      return null;
    }

    const configuredOrigin =
      process.env.NEXT_PUBLIC_SHORTSCALE_REDIRECT_ORIGIN;

    const origin = configuredOrigin
      ? configuredOrigin.replace(/\/+$/, "")
      : typeof window !== "undefined"
        ? window.location.origin
        : "";

    if (!origin) {
      return `/${result.short_code}`;
    }

    return `${origin}/${result.short_code}`;
  }, [result]);

  async function handleSubmit(
    event: FormEvent<HTMLFormElement>,
  ) {
    event.preventDefault();

    if (pending) {
      return;
    }

    const normalizedURL = url.trim();
    const validationError =
      validateURL(normalizedURL);

    setFieldError(validationError);
    setFormError(null);
    setCopyState("idle");

    if (validationError) {
      return;
    }

    setPending(true);

    try {
      const created = await createShortURL(
        normalizedURL,
      );

      setResult(created);
      onCreated?.();
    } catch (caught) {
      if (!(caught instanceof APIError)) {
        setFormError(
          "Couldn’t shorten this URL. Try again.",
        );
      } else if (caught.status === 401) {
        router.replace(
          "/auth/login?reason=expired",
        );
        return;
      } else if (caught.status === 429) {
        setFormError(
          getRateLimitMessage(
            caught.retryAfter,
          ),
        );
      } else if (caught.status === 400) {
        setFieldError(
          caught.message ||
            "Enter a valid HTTP or HTTPS URL.",
        );
      } else if (caught.status === 0) {
        setFormError(
          "ShortScale is temporarily unavailable. Try again.",
        );
      } else {
        setFormError(
          "Couldn’t shorten this URL. Try again.",
        );
      }

      setPending(false);
      return;
    }

    setPending(false);
  }

  async function handleCopy() {
    if (!shortURL) {
      return;
    }

    try {
      await navigator.clipboard.writeText(
        shortURL,
      );

      setCopyState("copied");

      window.setTimeout(() => {
        setCopyState("idle");
      }, 1800);
    } catch {
      setCopyState("error");
    }
  }

  return (
    <section
      className={styles.shortener}
      aria-labelledby="shortener-title"
    >
      <header className={styles.intro}>
        <h1
          className={styles.title}
          id="shortener-title"
        >
          Make a short link.
        </h1>

        <p className={styles.description}>
          Paste a long URL and get something
          cleaner to share.
        </p>
      </header>

      <form
        className={styles.form}
        onSubmit={handleSubmit}
        noValidate
      >
        <label
          className={styles.label}
          htmlFor="long-url"
        >
          Long URL
        </label>

        <div className={styles.formRow}>
          <input
            className={styles.input}
            id="long-url"
            name="url"
            type="url"
            inputMode="url"
            autoComplete="url"
            placeholder="https://example.com/a/very/long/path"
            value={url}
            onChange={(event) => {
              setURL(event.target.value);

              if (fieldError) {
                setFieldError(null);
              }
            }}
            aria-invalid={Boolean(fieldError)}
            aria-describedby={
              fieldError
                ? "long-url-error"
                : undefined
            }
            disabled={pending}
          />

          <button
            className={styles.submit}
            type="submit"
            disabled={pending}
          >
            {pending
              ? "Shortening…"
              : "Shorten"}
          </button>
        </div>

        {fieldError ? (
          <p
            className={styles.error}
            id="long-url-error"
            role="alert"
          >
            {fieldError}
          </p>
        ) : null}

        {formError ? (
          <p
            className={styles.error}
            role="alert"
          >
            {formError}
          </p>
        ) : null}
      </form>

      {result && shortURL ? (
        <div
          className={styles.result}
          aria-live="polite"
        >
          <p className={styles.resultLabel}>
            Short link
          </p>

          <div className={styles.resultPrimary}>
            <a
              className={styles.shortURL}
              href={shortURL}
              target="_blank"
              rel="noreferrer"
            >
              {shortURL}
            </a>

            <button
              className={styles.copyButton}
              type="button"
              onClick={handleCopy}
            >
              {copyState === "copied"
                ? "Copied"
                : "Copy"}
            </button>
          </div>

          <p className={styles.destination}>
            {result.original_url}
          </p>

          {copyState === "error" ? (
            <p
              className={styles.copyError}
              role="alert"
            >
              Couldn’t copy the link. Select it
              and copy it manually.
            </p>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
