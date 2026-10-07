#!/bin/bash
# Creates one folder per configuration, each with a copy of the compiled
# samples. Same as the original artifact script, but it can be run from any
# directory and re-run without errors.

cd "$(dirname "$0")"

for config in private shared mgvm mgvm-nobalance; do
  mkdir -p $config
  cp -r ../mgpusim/samples $config/
done
