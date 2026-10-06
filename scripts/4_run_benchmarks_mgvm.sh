#!/bin/bash
# Runs the 15 workloads with the mgvm configuration in the background.
# Progress is logged to mgvm/progress.log; check running jobs with ps or top.
cd "$(dirname "$0")"
nohup ./run_config.sh mgvm > mgvm/progress.log 2>&1 &
echo "started mgvm (pid $!), progress in mgvm/progress.log"
