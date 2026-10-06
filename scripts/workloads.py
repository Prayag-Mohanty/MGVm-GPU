"""Workloads of the MGvm evaluation (Table II) and their problem sizes.

Two presets are provided:

* ``default`` -- scaled-down inputs whose footprints still exceed the
  aggregate L2 TLB reach (4 chiplets x 512 entries x 4KB = 8MB). All 60 runs
  (15 workloads x 4 configurations) finish in a few hours on a 4-core machine
  with 16GB of RAM.
* ``paper`` -- inputs sized to approximate the memory footprints of Table II
  of the paper. Like the original artifact, these need hundreds of GB of RAM
  and up to a day per run.

Select a preset with the MGVM_PRESET environment variable.
"""

import os

# name: (script name, locality type, footprint in the paper (MB),
#        default args, paper args)
WORKLOADS = {
    "C2D": ("convolution2d", "NL", 512,
            "-n 2048 -m 2048", "-n 8192 -m 8192"),
    "FW": ("fastwalshtransform", "RCL", 32,
           "-n 1048576", "-n 4194304"),
    "GUPS": ("gups", "unclassified", 16,
             "-n 22 -m 16384 -iters 4", "-n 22 -m 262144 -iters 16"),
    "J1D": ("jacobi1d", "NL", 512,
            "-n 4194304 -iters 1", "-n 67108864 -iters 1"),
    "J2D": ("jacobi2d", "NL", 128,
            "-n 2048 -iters 1", "-n 4096 -iters 1"),
    "KM": ("kmeans", "ITL", 128,
           "-n 65536 -m 32 -iters 1", "-n 524288 -m 32 -iters 2"),
    "MIS": ("mis", "NL+ITL", 16,
            "-n 262144 -m 8", "-n 262144 -m 8"),
    "MT": ("matrixtranspose", "RCL", 32,
           "-n 2048", "-n 2048"),
    "PR": ("pagerank", "ITL", 32,
           "-n 65536 -m 16 -iters 2", "-n 524288 -m 8 -iters 4"),
    "SC": ("simpleconvolution", "NL", 512,
           "-n 2046 -m 2046", "-n 8190 -m 8190"),
    "RED": ("reduction", "NL", 256,
            "-n 8388608 -m 1024", "-n 67108864 -m 4096"),
    "SPMV": ("spmv", "ITL", 360,
             "-n 32768 -m 64", "-n 262144 -m 160"),
    "S2D": ("stencil2d", "NL", 32,
            "-n 1024 -m 1024 -iters 1", "-n 2048 -m 2048 -iters 1"),
    "SYRK": ("syrk", "RCL", 32,
             "-n 1024 -m 64", "-n 2048 -m 2048"),
    "SYR2": ("syr2k", "RCL", 16,
             "-n 1024 -m 32", "-n 1024 -m 1024"),
}

ORDER = ["C2D", "FW", "GUPS", "J1D", "J2D", "KM", "MIS", "MT", "PR", "SC",
         "RED", "SPMV", "S2D", "SYRK", "SYR2"]

CONFIGS = ["private", "shared", "mgvm-nobalance", "mgvm"]


def preset():
    p = os.environ.get("MGVM_PRESET", "default")
    if p not in ("default", "paper"):
        raise SystemExit("MGVM_PRESET must be 'default' or 'paper'")
    return p


def args_for(name):
    w = WORKLOADS[name]
    return w[3] if preset() == "default" else w[4]
