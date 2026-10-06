#!/usr/bin/env python3
"""Generates one script per workload and configuration.

For example, private/samples/convolution2d.sh runs C2D with private L2 TLBs.
Each script runs in its own directory (<config>/samples/<workload>/) and
leaves metrics.csv (MGPUSim metrics) and vm_stats.csv (virtual memory
statistics) there.
"""

import os
import stat

from workloads import CONFIGS, ORDER, WORKLOADS, args_for, preset

HERE = os.path.dirname(os.path.abspath(__file__))

TEMPLATE = """#!/bin/bash
# {name} ({desc}) with the {cfg} virtual memory configuration.
cd "$(dirname "$0")"
mkdir -p {script}
cd {script}
../mgvm -bench={name} {args} -vm={cfg} -report-all \\
  -metric-file-name=metrics -vm-stats=vm_stats.csv "$@" > run.log 2>&1
echo "exit code $?" >> run.log
"""


def main():
    for cfg in CONFIGS:
        samples = os.path.join(HERE, cfg, "samples")
        if not os.path.isdir(samples):
            raise SystemExit(f"{samples} missing, run ./2_copy_benchmarks.sh")
        for name in ORDER:
            script, loc, _, _, _ = WORKLOADS[name]
            path = os.path.join(samples, script + ".sh")
            with open(path, "w") as f:
                f.write(TEMPLATE.format(name=name, desc=loc, cfg=cfg,
                                        script=script, args=args_for(name)))
            os.chmod(path, os.stat(path).st_mode | stat.S_IEXEC | stat.S_IXGRP)
    print(f"generated runners for {len(ORDER)} workloads x {len(CONFIGS)} "
          f"configurations (preset: {preset()})")


if __name__ == "__main__":
    main()
