#!/bin/bash
# Removes the per-configuration folders and the collected results.
cd "$(dirname "$0")"
rm -rf private shared mgvm mgvm-nobalance results.csv normalized.csv figures
