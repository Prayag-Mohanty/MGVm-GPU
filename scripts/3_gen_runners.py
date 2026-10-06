#!/usr/bin/python3
"""Generates <config>/<benchmark>.sh for the 4 configurations x 15 workloads.

Usage: ./3_gen_runners.py [--preset paper|small]

  paper  The inputs used for the paper (default). Needs ~300-400GB of RAM
         per configuration and up to a day per run.
  small  Scaled-down inputs (largest allocation 8-32MB, still larger than the
         8MB aggregate L2 TLB reach) that finish in roughly 10-60 minutes per
         run with < 4GB of RAM each. The MGvm HSL and the HSL-aware allocator
         are scaled with the largest allocation exactly as for the paper
         inputs: custom-hsl = largest allocation / 4 chiplets / 4KB (at least
         512 pages = 2MB), hslaware-N with N = custom-hsl / 512.
"""

import argparse

parser = argparse.ArgumentParser()
parser.add_argument("--preset", choices=["paper", "small"], default="paper")
preset = parser.parse_args().preset

configs = ['private', 'shared', 'mgvm', 'mgvm-nobalance']

benchmarks = [
        'convolution2d',
        'fastwalshtransform',
        'gups',
        'jacobi1d',
        'jacobi2d',
        'kmeans',
        'matrixtranspose',
        'mis',
        'pagerank',
        'simpleconvolution',
        'shoc-reduction',
        'spmv',
        'stencil2d',
        'syrk',
        'syr2k',
        ]

# benchmark: (input arguments, custom-hsl in pages)
small_inputs = {
    'convolution2d': ("-ni=2048 -nj=2048 ", 1024),
    'fastwalshtransform': ("-length=2097152 ", 512),
    'gups': (" ", 1024),
    'jacobi1d': ("-n=4194304 -steps=1", 1024),
    'jacobi2d': ("-n=2048 -steps=1", 1024),
    'kmeans': ("-points=131072 -features=32 -clusters=20 -max-iter=1 ", 1024),
    'matrixtranspose': ("-width=2048 ", 1024),
    'mis': ("-numNodes=131072 -numItems=262144 ", 512),
    'pagerank': ("-node=4096 -sparsity=0.5 -iterations=1 ", 2048),
    'simpleconvolution': ("-width=2046 -height=2046 ", 1024),
    'shoc-reduction': ("-Size=4194304 -Iterations=1 ", 1024),
    'spmv': ("-dim=131072 -sparsity=0.0002 ", 512),
    'stencil2d': ("-row=2048 -col=2048 ", 1024),
    'syrk': ("-ni=1024 -nj=1024 ", 512),
    'syr2k': ("-ni=512 -nj=512 ", 512),
}


def write_small_hsl_and_inputs(f, config, benchmark):
    args, hsl = small_inputs[benchmark]
    if config == 'mgvm' or config == 'mgvm-nobalance':
        f.write("-custom-hsl %d " % hsl)
        f.write("-mem-allocator-type hslaware-%d " % (hsl // 512))
    # Like the paper inputs, SYRK and SYR2K are stopped after a fixed number
    # of instructions. The caps are lowered for the small preset because the
    # memory used by -report-all grows with the simulated time.
    if benchmark == 'syrk':
        f.write("-max-inst 3000000 ")
    if benchmark == 'syr2k':
        f.write("-max-inst 10000000 ")
    f.write(args)


for config in configs:
    for benchmark in benchmarks:
        print(config, benchmark)
        submit_file_name = config + '/' + benchmark + ".sh"
        submit_file = open(submit_file_name, "w")
        submit_file.write("#!/bin/bash\n")
        submit_file.write("cd samples\n")
        submit_file.write("cd " + benchmark + "\n")
        submit_file.write("./" + benchmark + " ")
        submit_file.write("-timing ")
        submit_file.write("-no-progress-bar ")
        submit_file.write("-report-all ")
        submit_file.write("-scheduling lasp ")

        if config == 'private':
            submit_file.write("-platform-type privatetlb ")
            submit_file.write("-mem-allocator-type lasp ")
            submit_file.write("-use-lasp-mem-alloc ")
        elif config == 'shared':
            submit_file.write("-platform-type xortlb ")
            submit_file.write("-mem-allocator-type lasp ")
            submit_file.write("-use-lasp-mem-alloc ")
            submit_file.write("-l2-tlb-striping 1 ")
        elif config == 'mgvm':
            submit_file.write("-platform-type customtlb ")
            submit_file.write("-use-lasp-hsl-mem-alloc ")
            submit_file.write("-switch-tlb-striping ")
        elif config == 'mgvm-nobalance':
            submit_file.write("-platform-type customtlb ")
            submit_file.write("-use-lasp-hsl-mem-alloc ")

        if benchmark == 'convolution2d':
            submit_file.write("-sched-partition Ydiv ")
        elif benchmark == 'fastwalshtransform':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'gups':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'jacobi1d':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'jacobi2d':
            submit_file.write("-sched-partition Ydiv ")
        elif benchmark == 'kmeans':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'matrixtranspose':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'mis':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'pagerank':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'simpleconvolution':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'shoc-reduction':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'spmv':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'stencil2d':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'syrk':
            submit_file.write("-sched-partition Xdiv ")
        elif benchmark == 'syr2k':
            submit_file.write("-sched-partition Xdiv ")

        if preset == 'small':
            write_small_hsl_and_inputs(submit_file, config, benchmark)
            submit_file.write("\n")
            submit_file.close()
            continue

        # set appropriate HSL values
        if config == 'mgvm' or config == 'mgvm-nobalance':
            if benchmark == 'convolution2d':
                submit_file.write("-custom-hsl 16384 ")
                submit_file.write("-mem-allocator-type hslaware-32 ")
            if benchmark == 'fastwalshtransform':
                submit_file.write("-custom-hsl 2048 ")
                submit_file.write("-mem-allocator-type hslaware-4 ")
            if benchmark == 'gups':
                submit_file.write("-custom-hsl 1024 ")
                submit_file.write("-mem-allocator-type hslaware-2 ")
            if benchmark == 'jacobi1d':
                submit_file.write("-custom-hsl 16384 ")
                submit_file.write("-mem-allocator-type hslaware-32 ")
            if benchmark == 'jacobi2d':
                submit_file.write("-custom-hsl 4096 ")
                submit_file.write("-mem-allocator-type hslaware-8 ")
            if benchmark == 'kmeans':
                submit_file.write("-custom-hsl 4096 ")
                submit_file.write("-mem-allocator-type hslaware-8 ")
            if benchmark == 'matrixtranspose':
                submit_file.write("-custom-hsl 1024 ")
                submit_file.write("-mem-allocator-type hslaware-2 ")
            if benchmark == 'mis':
                submit_file.write("-custom-hsl 512 ")
                submit_file.write("-mem-allocator-type hslaware-1 ")
            if benchmark == 'pagerank':
                submit_file.write("-custom-hsl 8192 ")
                submit_file.write("-mem-allocator-type hslaware-16 ")
            if benchmark == 'simpleconvolution':
                submit_file.write("-custom-hsl 16384 ")
                submit_file.write("-mem-allocator-type hslaware-32 ")
            if benchmark == 'shoc-reduction':
                submit_file.write("-custom-hsl 16384 ")
                submit_file.write("-mem-allocator-type hslaware-32 ")
            if benchmark == 'spmv':
                submit_file.write("-custom-hsl 512 ")
                submit_file.write("-mem-allocator-type hslaware-1 ")
            if benchmark == 'stencil2d':
                submit_file.write("-custom-hsl 1024 ")
                submit_file.write("-mem-allocator-type hslaware-2 ")
            if benchmark == 'syrk':
                submit_file.write("-custom-hsl 1024 ")
                submit_file.write("-mem-allocator-type hslaware-2 ")
            if benchmark == 'syr2k':
                submit_file.write("-custom-hsl 512 ")
                submit_file.write("-mem-allocator-type hslaware-1 ")

        # limit super long benchmarks
        if benchmark == 'syrk':
            submit_file.write("-max-inst 10000000 ")
        if benchmark == 'syr2k':
            submit_file.write("-max-inst 30000000 ")

        # set benchmark specific parameters
        if benchmark == 'convolution2d':
            submit_file.write("-ni=8192 -nj=8192 ")
        if benchmark == 'fastwalshtransform':
            submit_file.write("-length=8388608 ")
        if benchmark == 'gups':
            submit_file.write(" ")
        if benchmark == 'jacobi1d':
            submit_file.write("-n=67108864 -steps=1")
        if benchmark == 'jacobi2d':
            submit_file.write("-n=4096 -steps=1")
        if benchmark == 'kmeans':
            submit_file.write("-points=524288 -features=32 -clusters=20 -max-iter=1 ")
        if benchmark == 'matrixtranspose':
            submit_file.write("-width=2048 ")
        if benchmark == 'mis':
            submit_file.write("-numNodes=524288 -numItems=1048576 ")
        if benchmark == 'pagerank':
            submit_file.write("-node=8192 -sparsity=0.5 -iterations=1 ")
        if benchmark == 'simpleconvolution':
            submit_file.write("-width=8190 -height=8190 ")
        if benchmark == 'shoc-reduction':
            submit_file.write("-Size=67108864 -Iterations=2 ")
        if benchmark == 'spmv':
            submit_file.write("-dim=2097152 -sparsity=0.00001 ")
        if benchmark == 'stencil2d':
            submit_file.write("-row=2048 -col=2048 ")
        if benchmark == 'syrk':
            submit_file.write("-ni=2048 -nj=2048 ")
        if benchmark == 'syr2k':
            submit_file.write("-ni=1024 -nj=1024 ")
        submit_file.write("\n")
        submit_file.close()
