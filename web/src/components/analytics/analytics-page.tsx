"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  useEffect,
  useMemo,
  useState,
} from "react";

import { LogoutButton } from "@/components/auth/logout-button";
import { APIError } from "@/lib/api/auth";
import {
  getRedirectAnalytics,
  type RedirectAnalytics,
} from "@/lib/api/analytics";

import styles from "./analytics-page.module.css";

const RANGE_OPTIONS = [7, 30, 90] as const;

type AnalyticsPageProps = {
  shortCode: string;
};

type LoadState =
  | "loading"
  | "ready"
  | "not-found"
  | "error";

function publicShortURL(shortCode: string) {
  const configuredOrigin =
    process.env.NEXT_PUBLIC_SHORTSCALE_REDIRECT_ORIGIN;

  const origin = configuredOrigin
    ? configuredOrigin.replace(/\/+$/, "")
    : typeof window !== "undefined"
      ? window.location.origin
      : "";

  return origin
    ? `${origin}/${shortCode}`
    : `/${shortCode}`;
}

function formatTimestamp(value: string | null) {
  if (!value) {
    return "—";
  }

  const date = new Date(value);

  if (Number.isNaN(date.getTime())) {
    return "—";
  }

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

function formatChartDate(value: string) {
  const date = new Date(`${value}T00:00:00Z`);

  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  }).format(date);
}

function AnalyticsChart({
  analytics,
}: {
  analytics: RedirectAnalytics;
}) {
  const width = 720;
  const height = 260;
  const paddingX = 16;
  const paddingTop = 20;
  const paddingBottom = 34;

  const points = useMemo(() => {
    const values = analytics.daily;

    if (values.length === 0) {
      return [];
    }

    const maximum = Math.max(
      1,
      ...values.map((item) => item.redirects),
    );

    const usableWidth =
      width - paddingX * 2;

    const usableHeight =
      height - paddingTop - paddingBottom;

    return values.map((item, index) => {
      const x =
        values.length === 1
          ? width / 2
          : paddingX +
            (index / (values.length - 1)) *
              usableWidth;

      const y =
        paddingTop +
        usableHeight -
        (item.redirects / maximum) *
          usableHeight;

      return {
        ...item,
        x,
        y,
      };
    });
  }, [analytics.daily]);

  const path = points
    .map((point, index) => {
      const command =
        index === 0 ? "M" : "L";

      return `${command} ${point.x} ${point.y}`;
    })
    .join(" ");

  const firstPoint = points.at(0);
  const lastPoint = points.at(-1);

  return (
    <div className={styles.chartWrap}>
      <svg
        className={styles.chart}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-labelledby="analytics-chart-title analytics-chart-description"
      >
        <title id="analytics-chart-title">
          Redirects over the selected period
        </title>

        <desc id="analytics-chart-description">
          Visual chart of daily redirect counts for the selected analytics
          window. A complete text table follows the chart.
        </desc>

        <line
          className={styles.chartBaseline}
          x1={paddingX}
          y1={height - paddingBottom}
          x2={width - paddingX}
          y2={height - paddingBottom}
        />

        <path
          className={styles.chartLine}
          d={path}
          fill="none"
        />

        {points.map((point) => (
          <circle
            className={styles.chartPoint}
            cx={point.x}
            cy={point.y}
            key={point.date}
            r="3"
          >
            <title>
              {`${point.date}: ${point.redirects} redirects`}
            </title>
          </circle>
        ))}

        {firstPoint ? (
          <text
            className={styles.chartLabel}
            x={paddingX}
            y={height - 8}
          >
            {formatChartDate(
              firstPoint.date,
            )}
          </text>
        ) : null}

        {lastPoint ? (
          <text
            className={styles.chartLabel}
            x={width - paddingX}
            y={height - 8}
            textAnchor="end"
          >
            {formatChartDate(
              lastPoint.date,
            )}
          </text>
        ) : null}
      </svg>

      <table className="sr-only">
        <caption>
          Daily redirect counts for the last{" "}
          {analytics.window.days} days
        </caption>

        <thead>
          <tr>
            <th scope="col">Date</th>
            <th scope="col">Redirects</th>
          </tr>
        </thead>

        <tbody>
          {analytics.daily.map((item) => (
            <tr key={item.date}>
              <th scope="row">
                {item.date}
              </th>
              <td>{item.redirects}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function AnalyticsPage({
  shortCode,
}: AnalyticsPageProps) {
  const router = useRouter();

  const [days, setDays] =
    useState<(typeof RANGE_OPTIONS)[number]>(7);

  const [analytics, setAnalytics] =
    useState<RedirectAnalytics | null>(null);

  const [loadState, setLoadState] =
    useState<LoadState>("loading");

  const shortURL = useMemo(
    () => publicShortURL(shortCode),
    [shortCode],
  );

  useEffect(() => {
    let active = true;

    void getRedirectAnalytics(
      shortCode,
      days,
    ).then(
      (nextAnalytics) => {
        if (!active) {
          return;
        }

        setAnalytics(nextAnalytics);
        setLoadState("ready");
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
          caught.status === 404
        ) {
          setAnalytics(null);
          setLoadState("not-found");
          return;
        }

        setAnalytics(null);
        setLoadState("error");
      },
    );

    return () => {
      active = false;
    };
  }, [days, router, shortCode]);

  function changeRange(
    nextDays: (typeof RANGE_OPTIONS)[number],
  ) {
    if (nextDays === days) {
      return;
    }

    setLoadState("loading");
    setDays(nextDays);
  }

  function retry() {
    setLoadState("loading");

    void getRedirectAnalytics(
      shortCode,
      days,
    ).then(
      (nextAnalytics) => {
        setAnalytics(nextAnalytics);
        setLoadState("ready");
      },
      (caught: unknown) => {
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
          caught.status === 404
        ) {
          setAnalytics(null);
          setLoadState("not-found");
          return;
        }

        setAnalytics(null);
        setLoadState("error");
      },
    );
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <Link
          className={styles.brand}
          href="/"
        >
          ShortScale
        </Link>

        <nav
          className={styles.nav}
          aria-label="Product navigation"
        >
          <Link
            className={styles.navLink}
            href="/#recent-links"
          >
            My links
          </Link>

          <LogoutButton />
        </nav>
      </header>

      <main className={styles.content}>
        {loadState === "not-found" ? (
          <section
            className={styles.state}
            aria-labelledby="analytics-not-found"
          >
            <p className={styles.eyebrow}>
              Analytics
            </p>

            <h1
              className={styles.stateTitle}
              id="analytics-not-found"
            >
              Analytics not found.
            </h1>

            <p className={styles.stateCopy}>
              This short link does not exist or
              is not available to your account.
            </p>

            <Link
              className={styles.backLink}
              href="/#recent-links"
            >
              Back to my links
            </Link>
          </section>
        ) : null}

        {loadState === "error" ? (
          <section
            className={styles.state}
            aria-labelledby="analytics-error"
          >
            <p className={styles.eyebrow}>
              Analytics
            </p>

            <h1
              className={styles.stateTitle}
              id="analytics-error"
            >
              Couldn’t load analytics.
            </h1>

            <p className={styles.stateCopy}>
              ShortScale is temporarily unable
              to load this link’s analytics.
            </p>

            <button
              className={styles.retryButton}
              type="button"
              onClick={retry}
            >
              Retry
            </button>
          </section>
        ) : null}

        {loadState === "loading" ? (
          <section
            className={styles.loading}
            role="status"
            aria-live="polite"
          >
            <span className="sr-only">
              Loading analytics.
            </span>

            <div
              className={styles.loadingTitle}
              aria-hidden="true"
            />

            <div
              className={styles.loadingMetric}
              aria-hidden="true"
            />

            <div
              className={styles.loadingChart}
              aria-hidden="true"
            />
          </section>
        ) : null}

        {loadState === "ready" &&
        analytics ? (
          <section
            className={styles.analytics}
            aria-labelledby="analytics-title"
          >
            <div className={styles.intro}>
              <Link
                className={styles.backLink}
                href="/#recent-links"
              >
                ← My links
              </Link>

              <p className={styles.eyebrow}>
                Analytics
              </p>

              <h1
                className={styles.title}
                id="analytics-title"
              >
                {analytics.short_code}
              </h1>

              <a
                className={styles.shortURL}
                href={shortURL}
                target="_blank"
                rel="noreferrer"
              >
                {shortURL}
              </a>
            </div>

            <div className={styles.metricBlock}>
              <p className={styles.metricLabel}>
                Total redirects
              </p>

              <p className={styles.metricValue}>
                {analytics.total_redirects.toLocaleString()}
              </p>

              <div className={styles.metricMeta}>
                <div>
                  <span>
                    First redirect
                  </span>

                  <strong>
                    {formatTimestamp(
                      analytics.first_redirect_at,
                    )}
                  </strong>
                </div>

                <div>
                  <span>
                    Latest redirect
                  </span>

                  <strong>
                    {formatTimestamp(
                      analytics.last_redirect_at,
                    )}
                  </strong>
                </div>
              </div>
            </div>

            <div className={styles.chartHeader}>
              <div>
                <h2 className={styles.chartTitle}>
                  Redirect activity
                </h2>

                <p className={styles.chartSummary}>
                  {analytics.window.redirects.toLocaleString()}{" "}
                  redirects in the last{" "}
                  {analytics.window.days} days
                </p>
              </div>

              <div
                className={styles.rangeGroup}
                role="group"
                aria-label="Analytics time range"
              >
                {RANGE_OPTIONS.map(
                  (option) => (
                    <button
                      className={
                        option === days
                          ? `${styles.rangeButton} ${styles.rangeButtonActive}`
                          : styles.rangeButton
                      }
                      key={option}
                      type="button"
                      aria-pressed={
                        option === days
                      }
                      onClick={() =>
                        changeRange(option)
                      }
                    >
                      {option}d
                    </button>
                  ),
                )}
              </div>
            </div>

            {analytics.window.redirects ===
            0 ? (
              <div className={styles.zeroState}>
                <p>
                  No redirects in this period.
                </p>

                <p>
                  Activity will appear here
                  after someone opens this
                  short link.
                </p>
              </div>
            ) : (
              <AnalyticsChart
                analytics={analytics}
              />
            )}
          </section>
        ) : null}
      </main>
    </div>
  );
}
