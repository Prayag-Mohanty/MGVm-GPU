#!/bin/bash
# Runs all four configurations one after another (MGVM_JOBS jobs at a time).
cd "$(dirname "$0")"
for cfg in private shared mgvm-nobalance mgvm; do
  ./run_config.sh $cfg > $cfg/progress.log 2>&1
done
