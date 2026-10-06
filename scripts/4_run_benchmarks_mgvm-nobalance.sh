#!/bin/bash
# Runs the 15 workloads with the mgvm-nobalance configuration in the background.
# Progress is logged to mgvm-nobalance/progress.log; check running jobs with ps or top.
cd "$(dirname "$0")"
nohup ./run_config.sh mgvm-nobalance > mgvm-nobalance/progress.log 2>&1 &
echo "started mgvm-nobalance (pid $!), progress in mgvm-nobalance/progress.log"
