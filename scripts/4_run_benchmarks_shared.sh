#!/bin/bash
# Runs the 15 workloads with the shared configuration in the background.
# Progress is logged to shared/progress.log; check running jobs with ps or top.
cd "$(dirname "$0")"
nohup ./run_config.sh shared > shared/progress.log 2>&1 &
echo "started shared (pid $!), progress in shared/progress.log"
