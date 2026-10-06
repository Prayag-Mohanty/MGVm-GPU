#!/bin/bash
# Runs the 15 workloads with the private configuration in the background.
# Progress is logged to private/progress.log; check running jobs with ps or top.
cd "$(dirname "$0")"
nohup ./run_config.sh private > private/progress.log 2>&1 &
echo "started private (pid $!), progress in private/progress.log"
