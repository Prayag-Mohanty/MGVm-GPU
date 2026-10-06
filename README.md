# MGvm reimplementation on MGPUSim v3 (independent, not the authors' code)

This branch is an independent reimplementation of the MCM-GPU virtual memory
proposed in *Designing Virtual Memory System of MCM GPUs* (MICRO 2022), written
from the paper before the original artifact was available. The authors'
artifact is on the `main` branch of this repository and is the reference.

It is built on upstream MGPUSim v3.0.3 (akita v3) and adds:

- `mgpusim/mgvm/`: HSL functions (private, 4KB interleave, dHSL-coarse), L2 TLB
  slices (512 entries, 8-way, 10-cycle, 64 MSHRs), RTUs with epoch-based
  imbalance detection, page walkers (16 per chiplet, 32-entry PWC) that read
  PTEs through the local L2 cache or RDMA, page-table page placement
  (first-placement / by HSL), a 32-cycle inter-chiplet network, and the
  runtime implementing Listing 1 (per-kernel dHSL-coarse) and Listing 2
  (switch to dHSL-balance and back).
- `mgpusim/driver/`: LASP-style block data placement and contiguous CTA
  scheduling for unified multi-chiplet GPUs, kernel-launch hook.
- `mgpusim/benchmarks/mgvm/`: C2D, J1D, J2D, SYRK, SYR2, GUPS, RED, MIS kernels
  in OpenCL, compiled with stock clang-18 (`-nogpulib`, gfx803, code object
  v4) and loaded by `kernels.LoadProgramFromMemoryCOV4`.
- `mgpusim/samples/mgvm`: one binary for all 15 workloads.

```
cd mgpusim/samples/mgvm && go build
./mgvm -list
./mgvm -bench=SYRK -vm=mgvm -report-all        # writes metrics.csv, vm_stats.csv
```

`-vm` is one of `private`, `shared`, `mgvm-nobalance`, `mgvm`.
