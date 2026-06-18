#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import sys


def seconds_until(hour: int, minute: int) -> int:
    now = dt.datetime.now()
    target = now.replace(hour=hour, minute=minute, second=0, microsecond=0)
    if target <= now:
        target += dt.timedelta(days=1)
    return max(1, int((target - now).total_seconds()))


if __name__ == "__main__":
    print(seconds_until(int(sys.argv[1]), int(sys.argv[2])))
