#!/usr/bin/env python3
import argparse
import json
import time
import urllib.parse
import urllib.request
from pathlib import Path

DEFAULT_PROMETHEUS = "http://127.0.0.1:19090"

QUERIES = {
    "api_enqueue_enqueued": (
        'sum(shortscale_redirect_analytics_enqueues_total{result="enqueued"})'
    ),
    "api_enqueue_dropped": (
        'sum(shortscale_redirect_analytics_enqueues_total{result="dropped"})'
    ),
    "api_publish_success": (
        'sum(shortscale_redirect_analytics_publishes_total{result="success"})'
    ),
    "api_publish_failure": (
        'sum(shortscale_redirect_analytics_publishes_total{result="failure"})'
    ),
    "api_queue_depth": "sum(shortscale_redirect_analytics_queue_depth)",
    "consumer_processed": "sum(shortscale_analytics_consumer_processed_total)",
    "consumer_persistence_inserted": (
        'sum(shortscale_analytics_consumer_persistence_total{result="inserted"})'
    ),
    "consumer_persistence_duplicate": (
        'sum(shortscale_analytics_consumer_persistence_total{result="duplicate"})'
    ),
    "consumer_persistence_error": (
        'sum(shortscale_analytics_consumer_persistence_total{result="error"})'
    ),
    "consumer_failures": "sum(shortscale_analytics_consumer_failures_total)",
    "consumer_lag": "sum(shortscale_analytics_consumer_lag_records)",
}


def query(prometheus: str, expression: str) -> float:
    params = urllib.parse.urlencode({"query": expression})
    url = f"{prometheus.rstrip('/')}/api/v1/query?{params}"

    with urllib.request.urlopen(url, timeout=5) as response:
        payload = json.load(response)

    if payload.get("status") != "success":
        raise RuntimeError(f"Prometheus query failed: {expression}")

    result = payload.get("data", {}).get("result", [])

    if not result:
        return 0.0

    return sum(float(item["value"][1]) for item in result)


def snapshot(prometheus: str) -> dict:
    values = {name: query(prometheus, expr) for name, expr in QUERIES.items()}
    return {
        "captured_at_unix": time.time(),
        "prometheus": prometheus,
        "metrics": values,
    }


def write_snapshot(payload: dict, output: str | None) -> None:
    text = json.dumps(payload, indent=2, sort_keys=True)

    if output:
        path = Path(output)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text + "\n", encoding="utf-8")
        print(path)
    else:
        print(text)


def delta(before_path: str, after_path: str) -> dict:
    before = json.loads(Path(before_path).read_text(encoding="utf-8"))
    after = json.loads(Path(after_path).read_text(encoding="utf-8"))

    before_metrics = before["metrics"]
    after_metrics = after["metrics"]

    counters = [
        name
        for name in QUERIES
        if name not in {"api_queue_depth", "consumer_lag"}
    ]

    counter_deltas = {
        name: after_metrics.get(name, 0.0) - before_metrics.get(name, 0.0)
        for name in counters
    }

    return {
        "before": before_path,
        "after": after_path,
        "counter_deltas": counter_deltas,
        "final_gauges": {
            "api_queue_depth": after_metrics.get("api_queue_depth", 0.0),
            "consumer_lag": after_metrics.get("consumer_lag", 0.0),
        },
    }


def wait_drained(
    prometheus: str,
    timeout_seconds: int,
    interval_seconds: float,
    stable_polls: int,
) -> None:
    deadline = time.monotonic() + timeout_seconds
    stable = 0

    while True:
        queue_depth = query(
            prometheus,
            QUERIES["api_queue_depth"],
        )
        consumer_lag = query(
            prometheus,
            QUERIES["consumer_lag"],
        )

        print(
            f"queue_depth={queue_depth:g} "
            f"consumer_lag={consumer_lag:g} "
            f"stable={stable}/{stable_polls}"
        )

        if queue_depth == 0 and consumer_lag == 0:
            stable += 1
            if stable >= stable_polls:
                return
        else:
            stable = 0

        if time.monotonic() >= deadline:
            raise SystemExit("timed out waiting for analytics pipeline to drain")

        time.sleep(interval_seconds)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Final benchmark Prometheus metric snapshots and drain checks."
    )
    parser.add_argument(
        "--prometheus",
        default=DEFAULT_PROMETHEUS,
        help=f"Prometheus base URL (default: {DEFAULT_PROMETHEUS})",
    )

    subparsers = parser.add_subparsers(dest="command", required=True)

    snapshot_parser = subparsers.add_parser("snapshot")
    snapshot_parser.add_argument("--output")

    delta_parser = subparsers.add_parser("delta")
    delta_parser.add_argument("before")
    delta_parser.add_argument("after")
    delta_parser.add_argument("--output")

    drain_parser = subparsers.add_parser("wait-drained")
    drain_parser.add_argument("--timeout", type=int, default=180)
    drain_parser.add_argument("--interval", type=float, default=2.0)
    drain_parser.add_argument("--stable-polls", type=int, default=3)

    args = parser.parse_args()

    if args.command == "snapshot":
        write_snapshot(snapshot(args.prometheus), args.output)
        return

    if args.command == "delta":
        write_snapshot(
            delta(args.before, args.after),
            args.output,
        )
        return

    if args.command == "wait-drained":
        wait_drained(
            args.prometheus,
            args.timeout,
            args.interval,
            args.stable_polls,
        )
        return

    raise SystemExit(f"unknown command: {args.command}")


if __name__ == "__main__":
    main()
