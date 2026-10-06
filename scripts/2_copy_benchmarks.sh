#!/bin/bash
# Creates one directory per configuration, each with a samples folder holding
# the simulator binary.
set -e
cd "$(dirname "$0")"
if [ ! -x bin/mgvm ]; then
  echo "bin/mgvm not found, run ./1_compile_benchmarks.py first" >&2
  exit 1
fi
for cfg in private shared mgvm-nobalance mgvm; do
  mkdir -p "$cfg/samples"
  cp bin/mgvm "$cfg/samples/mgvm"
done
echo "copied the simulator to private/ shared/ mgvm-nobalance/ mgvm/"
