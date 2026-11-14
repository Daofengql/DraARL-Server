#!/usr/bin/env python3
"""Capture admin UDP counters as NDJSON. DRAARL_TOKEN holds the admin JWT."""
import argparse
import json
import os
import time
import urllib.request
from datetime import datetime, timezone


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base_url")
    parser.add_argument("output")
    parser.add_argument("--interval", type=float, default=10)
    parser.add_argument("--samples", type=int, default=0, help="0 captures until interrupted")
    args = parser.parse_args()
    token = os.environ["DRAARL_TOKEN"]
    request = urllib.request.Request(
        args.base_url.rstrip("/") + "/api/admin/udp/metrics",
        headers={"Authorization": "Bearer " + token},
    )
    count = 0
    with open(args.output, "a", encoding="utf-8") as output:
        try:
            while args.samples == 0 or count < args.samples:
                with urllib.request.urlopen(request, timeout=10) as response:
                    sample = json.load(response)
                output.write(json.dumps({"captured_at": datetime.now(timezone.utc).isoformat(),
                                         "metrics": sample["data"]}, ensure_ascii=False) + "\n")
                output.flush()
                count += 1
                if args.samples == 0 or count < args.samples:
                    time.sleep(max(0.1, args.interval))
        except KeyboardInterrupt:
            pass


if __name__ == "__main__":
    main()
