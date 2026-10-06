#!/usr/bin/env python3
"""Compiles the benchmark kernels (if clang is available) and the simulator.

The OpenCL kernels of the new workloads are compiled to gfx803 code objects
with a stock clang (the compiled .hsaco files are also checked in, so this
step is optional). The simulator and all workloads are linked into a single
Go binary, bin/mgvm.
"""

import os
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
SIM = os.path.join(HERE, "..", "mgpusim")


def run(cmd, cwd):
    print("+", " ".join(cmd), flush=True)
    subprocess.run(cmd, cwd=cwd, check=True)


def main():
    if shutil.which("clang") and "--skip-kernels" not in sys.argv:
        run(["make", "-C", os.path.join(SIM, "benchmarks", "mgvm")], HERE)
    else:
        print("clang not found or --skip-kernels given: "
              "using the checked-in .hsaco files")

    os.makedirs(os.path.join(HERE, "bin"), exist_ok=True)
    run(["go", "build", "-o", os.path.join(HERE, "bin", "mgvm"),
         "./samples/mgvm"], SIM)
    print("built", os.path.join(HERE, "bin", "mgvm"))


if __name__ == "__main__":
    main()
