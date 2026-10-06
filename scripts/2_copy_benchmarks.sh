#!/bin/bash
# Creates one folder per configuration, each with a copy of the compiled
# samples. (The original artifact script also removed the folders again at the
# end, which made 3_gen_runners.py fail; those lines were moved to
# 0_clean.sh.)

cd "$(dirname "$0")"

for config in private shared mgvm mgvm-nobalance; do
  mkdir -p $config
  cp -r ../mgpusim/samples $config/
done
