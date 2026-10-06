#!/bin/bash
# Runs all the workload scripts of one configuration, at most MGVM_JOBS
# (default: number of cores) at a time. Usage: run_config.sh <config>
cd "$(dirname "$0")"
cfg=$1
jobs=${MGVM_JOBS:-$(nproc)}
if [ ! -d "$cfg/samples" ]; then
  echo "$cfg/samples missing, run steps 1-3 first" >&2
  exit 1
fi
ls "$cfg"/samples/*.sh | xargs -P "$jobs" -I{} bash -c 'start=$(date +%s); {}; echo "$(date) {} done in $(( $(date +%s) - start ))s"'
