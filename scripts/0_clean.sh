#!/bin/bash
# Removes the per-configuration run directories, binaries and results.
cd "$(dirname "$0")"
rm -rf private shared mgvm mgvm-nobalance bin results.csv normalized.csv figures
echo "cleaned"
