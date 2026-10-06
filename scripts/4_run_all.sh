#!/bin/bash
# Runs all 60 simulations (4 configurations x 15 workloads), at most
# MGVM_JOBS (default: number of cores) at a time. Use this instead of the
# 4_run_benchmarks_<config>.sh scripts, which start 15 simulations at once
# per configuration, on machines with few cores or little memory. With
# -report-all a simulation of the small preset needs up to ~4GB of RAM.
#
# Runs that already completed (metrics.csv present) are skipped, so the
# script can be re-run to resume.
#
#   MGVM_JOBS=3 nohup ./4_run_all.sh > run_all.log 2>&1 &

cd "$(dirname "$0")"
jobs=${MGVM_JOBS:-$(nproc)}

for config in private shared mgvm-nobalance mgvm; do
  for f in $config/*.sh; do
    benchmark=$(basename $f .sh)
    if [ -s $config/samples/$benchmark/metrics.csv ]; then
      echo "skipping $config $benchmark (already done)" >&2
      continue
    fi
    echo "$config $benchmark"
  done
done | xargs -P "$jobs" -L1 bash -c '
  config=$0; benchmark=$1
  start=$(date +%s)
  (cd $config && bash ${benchmark}.sh > ${benchmark}.output 2>&1)
  rc=$?
  echo "$(date +%T) $config $benchmark exit=$rc $(( $(date +%s) - start ))s"
'
