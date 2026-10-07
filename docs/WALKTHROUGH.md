# Walkthrough: the MGvm artifact, the code, and what was done

This document explains the repository for someone who has to present it: what the paper proposes,
where each idea lives in the authors' code, how a simulation is run end to end, what was changed
on top of the original artifact, and what the results show.

Paper: Pratheek B, Neha Jawalkar, Arkaprava Basu, *Designing Virtual Memory System of MCM GPUs*,
MICRO 2022. Artifact: Zenodo record [10.5281/zenodo.6937470](https://doi.org/10.5281/zenodo.6937470).

---

## 1. The paper in five sentences

1. A multi-chip-module (MCM) GPU is built from several smaller dies (**chiplets**, 4 here), each
   with its own CUs, L2 cache, DRAM, **L2 TLB slice** and **page-table walkers**; reaching another
   chiplet costs extra interconnect latency (~32 ns each way).
2. The L2 TLB can be **private** (each slice only caches translations for its own chiplet:
   fast hits, but entries are duplicated and capacity is wasted) or **shared** (one logical TLB:
   a hash called the **HSL, Home Slice Selection** function picks the slice for each virtual page:
   no duplication, but most lookups go to a remote chiplet).
3. Either way, **page walks** often read page-table entries (PTEs) stored in a remote chiplet's
   memory, which makes walks slow.
4. **MGvm** fixes both: it chooses the HSL **per kernel** (dHSL) to match the data placement that
   LASP already uses (so data that a chiplet uses locally is also translated locally), uses a
   **coarse 2 MB-multiple granularity** (dHSL-coarse) so that the 4 KB page holding the leaf PTEs
   of a 2 MB region can be placed on the same chiplet that translates that region, and
   **switches to fine 4 KB interleaving at runtime** (dHSL-balance) if one slice gets overloaded.
5. Result in the paper: +52 % over private TLB and +30 % over shared TLB (geomean, 15 workloads).

Terms used below:

| Term | Meaning |
|---|---|
| CTA / work-group (WG) | A block of GPU threads; scheduled onto one CU |
| LASP | Locality-Aware Scheduling and Placement (Khairy et al., MICRO'20): puts CTAs and the data they touch on the same chiplet. All four configurations use it. |
| HSL | Function `virtual page -> chiplet` that decides which L2 TLB slice (and walkers) serve a translation |
| RTU | Remote Translation Unit: per-chiplet unit that forwards TLB misses to other chiplets and back |
| PWC | Page-walk cache: caches upper levels of the page table inside the walker |
| MPKI | L2 TLB misses per thousand instructions |

---

## 2. Repository map

```
MGVm-GPU/
├── README.md                 How to run + results summary
├── docs/WALKTHROUGH.md       This file
├── mgvm.Dockerfile           Original artifact Dockerfile (needs mgvm.tar.gz from Zenodo)
├── Dockerfile                Same environment, built from this repository
├── mgpusim/                  The authors' modified MGPUSim (GPU simulator, Go)
│   ├── samples/<workload>/   One main.go per workload; the binary is the simulator + workload
│   ├── samples/runner/       Command-line flags, platform selection, metrics (runner.go)
│   ├── platform/             Top-level platform builders: privatetlb.go, xortlb.go, customtlb.go, ...
│   ├── builders/             Builds the GPU: 4 chiplets, their TLBs, MMUs, RTUs, caches, network
│   ├── remotetranslation/    The RTU (remotetranslationunit.go)
│   ├── timing/cp/            Command processor: LASP CTA scheduling and the MGvm switch decision
│   ├── benchmarks/           GPU kernels (.hsaco) + host code for each workload
│   └── driver/               GPU driver model (memory allocation, kernel launch)
├── mem/                      Akita memory library, modified by the authors
│   ├── vm/tlb/               L1/L2 TLB models (tlb_with_latency.go = L2 TLB slice "LatTLB")
│   ├── vm/mmu/               Page-table walkers (mmu.go) with the page-walk cache
│   ├── device/               Radix page table with real physical addresses + memory allocators
│   └── cache/                Low-module finders = the HSL routing (twolevellowmodulefinder.go)
├── akita/                    Akita discrete-event simulation engine
├── noc/                      Network-on-chip models (the inter-chiplet network "chipnetwork")
├── util/, dnn/, vis/         Supporting Akita modules (tracing, DNN benchmarks, visualisation)
├── scripts/                  Evaluation workflow (section 5)
└── results/small/            Our results: CSVs, figures, raw metrics, run log
```

Branch `reimplementation-mgpusim-v3` holds a separate, independent reimplementation (section 8).

---

## 3. How the simulator is built (the hardware model)

The authors model the whole MCM GPU as **one GPU containing 4 chiplets** (upstream MGPUSim would
model 4 separate GPUs). Per chiplet (Table I of the paper): 32 CUs (8 shader arrays x 4 CUs),
16 memory banks with an L2 cache, one L2 TLB slice, one MMU (page walkers), one RTU, and RDMA
engines that carry remote data and PTE requests over the inter-chiplet network.

### 3.1 Choosing the configuration — `mgpusim/samples/runner/runner.go`

Every workload binary accepts the same flags (lines ~104–131). The ones the scripts use:

| Flag | Effect |
|---|---|
| `-platform-type privatetlb / xortlb / customtlb` | Picks the platform builder (switch at line ~623; cases at ~948, ~1035, ~1315) |
| `-scheduling lasp -sched-partition Xdiv\|Ydiv` | LASP CTA scheduling, split the grid along X or Y |
| `-mem-allocator-type lasp` | Data placement by LASP; page-table pages follow the data (baseline) |
| `-mem-allocator-type hslaware-N` | MGvm page-table placement, N = HSL granularity in 2 MB units |
| `-custom-hsl P` | MGvm's dHSL-coarse granularity, in 4 KB pages (512 pages = 2 MB) |
| `-l2-tlb-striping 1` | Shared TLB: interleave slices every page |
| `-switch-tlb-striping` | Enables dHSL-balance (the only difference between `mgvm` and `mgvm-nobalance`) |
| `-max-inst N` | Stop after N instructions (used for SYRK/SYR2K) |
| `-report-all` | Write all statistics to `metrics.csv` |

### 3.2 The four configurations in code

All three platform builders (`platform/privatetlb.go`, `xortlb.go`, `customtlb.go`) create the same
GPU skeleton and differ in **how an L1 TLB miss finds its L2 TLB slice** (the HSL).

* **Private** — `builders/privatetlb.go`: L1 TLBs always talk to the local L2 TLB slice; no RTU is
  configured, so there are never remote L2 TLB hits.
* **Shared** — `builders/xortlb.go` (`connectL1TLBToL2TLB`, line ~138): a `LocalXORLowModuleFinder`
  hashes the virtual page number to a chiplet. If the result is the local chiplet the request goes
  to the local slice, otherwise to the RTU, which forwards it to the home chiplet's RTU and slice.
* **MGvm** — `builders/customtlb.go`. The HSL is defined in two places (lines ~44 and ~124):
  ```go
  hsl := func(address uint64) uint64 {
      return (address / uint64(b.customHSLpmdUnits)) % uint64(b.numChiplet)
  }
  ```
  `address` here is the virtual page number (the finder shifts out the 12 page-offset bits,
  `mem/cache/twolevellowmodulefinder.go:134`), so `-custom-hsl 1024` means: 1024 consecutive
  pages (4 MB) map to chiplet 0, the next 4 MB to chiplet 1, and so on round the 4 chiplets.
  This is **dHSL-coarse**. The same function is given to the L1-side finder (local vs. RTU), the
  RTU's remote table, and the L2 TLB (`HashFuncHSL`).

### 3.3 Life of a memory access (MGvm configuration)

```
CU ──> L1 TLB ──miss──> CustomTwoLevelLowModuleFinder: hsl(VPN) == my chiplet?
                           ├─ yes ─> local L2 TLB slice (LatTLB, 64 sets x 8 ways = 512 entries)
                           └─ no ──> local RTU ──network──> home RTU ──> home L2 TLB slice
L2 TLB slice ──miss──> its chiplet's MMU (16 concurrent walks, page-walk cache)
MMU ──for each level not in the PWC──> 8-byte read of the PTE at its *physical* address
        ├─ PTE page on this chiplet  -> local L2 cache / DRAM  ("page_walk_req_local")
        └─ PTE page elsewhere        -> ChipRDMA over the network ("page_walk_req_remote")
response travels back the same way; the L1 TLB fills and the CU's access proceeds
```

* L2 TLB slice: `mem/vm/tlb/tlb_with_latency.go` (`LatTLB`), built in
  `builders/common.go` `buildL2TLB` (64 sets, 8 ways).
* Page walker: `mem/vm/mmu/mmu.go`. `sendToMem` (line ~285) computes the physical address of the
  PTE for the current level, sends the read, and classifies it as local or remote by whether the
  destination is the `ChipRDMA` engine. These two counters become Figure 9.
  `WithMaxNumReqInFlight(16)` in `builders/common.go` = 16 walkers per chiplet.
* RTU: `mgpusim/remotetranslation/remotetranslationunit.go`.

### 3.4 Where page-table pages live — `mem/device/`

The authors replaced MGPUSim's flat map with a **4-level radix page table whose nodes have real
physical addresses** (`mem/device/pagetable.go`, `newTreeNode` ~line 180). Whenever a new table
node is created, the memory allocator decides which chiplet's memory holds it
(`allocatePageTablePage`):

* `lasp` allocator (`devicelaspmemstate.go:95`): the page goes to the chiplet that holds the data
  page that caused it to be created (**first placement**, the baseline policy).
* `hslaware-N` allocator (`devicehslmemstate.go:102`): `chiplet = (VA >> 21) / N % 4`. For the node
  that holds leaf PTEs, `VA >> 21` is the 2 MB region index, so the leaf-PTE page is put on exactly
  the chiplet that dHSL-coarse makes responsible for translating that region (**MGvm step 2**).
  That is why the scripts always pair `-custom-hsl P` with `hslaware-(P/512)`.

### 3.5 LASP scheduling — `mgpusim/timing/cp/internal/dispatching/lasp_alg.go`

Before dispatch, every work-group is assigned to a chiplet: with `Xdiv` chiplet =
`WG.IDX / ceil(numWGX / 4)`, with `Ydiv` chiplet = `WG.IDY / (numWGY / 4)` (lines ~52–100). Each
chiplet's CUs then take WGs only from their own list. (The `30 127 3` lines in every run's output
are this code printing `IDX IDY chiplet` for each work-group.)

### 3.6 dHSL-balance (runtime switching) — Listing 2 of the paper

1. **RTU epoch** (`remotetranslationunit.go:138`, `consolidateStats`): every 5000 serviced requests
   the RTU closes an epoch; after two consecutive "imbalanced" epochs it sends a `TriggerMsg` to the
   command processor.
2. **Command processor** (`timing/cp/commandprocessor.go`): on a trigger it asks all L2 TLBs, MMUs
   and RTUs for their counters (`requestStatsFromComponents`); when all 12 replies are in,
   `processCollectedStats` (~line 1168) computes the L2 TLB hit rate and checks if any chiplet
   receives more than 80 % of all incoming remote requests (`getImbalance`, line ~1153). If the hit
   rate is > 0.9 **and** imbalance is seen twice in a row (`switchTo4kHysteris`), it calls
   `sendTLBIndexingSwitchMsg` (line ~1038). This prints the `L2 TLB Hit Rate` and
   `Switching: Num Incoming ...` lines seen in the logs.
3. **Switch** (`RTU.switchIndexing`, line ~203; `LatTLB.switchIndexing`,
   `tlb_with_latency.go:755`): every component replaces its hash function with a 4 KB hash and
   keeps working; requests that arrive at the "wrong" slice during the transition are re-routed.
4. **Switch back**: the L2 TLB counts, per epoch, how its accesses would map under dHSL-coarse
   (`perEpochWhatIf`, `tlb_with_latency.go:379`), which the paper uses to detect that the imbalance
   has gone.

Two implementation details worth knowing (they differ from the paper's text):

* In `consolidateStats` the RTU-side test `Incoming > 2*Outgoing` is computed and then overwritten
  by `imbalanced = true` (line ~140). So the RTUs trigger the command processor every second epoch
  regardless, and the real decision is the command processor's 80 % / 0.9 check.
* The fine-grain HSL used after switching is an XOR hash of four 2-bit fields of the page number
  (`HashFunc4KXOR`), not a plain `page % 4`.
* The inter-chiplet network is built with `WithSwitchLatency(360)` at 1 GHz
  (`builders/common.go:940`). How this maps onto the paper's "~32 ns" is worth confirming with the
  authors; we did not change it.

---

## 4. The workloads

`samples/<name>/main.go` builds the platform via the runner and runs one benchmark from
`benchmarks/`. The 15 used in the paper (Table II):

| Paper | Sample folder | Suite | LASP class | Partition |
|---|---|---|---|---|
| C2D | convolution2d | PolyBench | NL | Ydiv |
| FW | fastwalshtransform | AMD APP SDK | RCL | Xdiv |
| GUPS | gups | random access | unclassified | Xdiv |
| J1D / J2D | jacobi1d / jacobi2d | PolyBench | NL | Xdiv / Ydiv |
| KM | kmeans | Hetero-Mark | ITL | Xdiv |
| MIS | mis | Pannotia | NL+ITL | Xdiv |
| MT | matrixtranspose | AMD APP SDK | RCL | Xdiv |
| PR | pagerank | Hetero-Mark | ITL | Xdiv |
| SC | simpleconvolution | AMD APP SDK | NL | Xdiv |
| RED | shoc-reduction | SHOC | NL | Xdiv |
| SPMV | spmv | SHOC | ITL | Xdiv |
| S2D | stencil2d | SHOC | NL | Xdiv |
| SYRK / SYR2 | syrk / syr2k | PolyBench | RCL | Xdiv |

(NL = no locality across CTAs, RCL = row/column locality, ITL = intra-thread locality.)

---

## 5. The evaluation workflow — `scripts/`

| Step | Script | What it does |
|---|---|---|
| 0 | `0_clean.sh` | Deletes the per-configuration folders and results |
| 1 | `1_compile_benchmarks.py` | `go build` in each of the 15 `mgpusim/samples/<workload>` folders |
| 2 | `2_copy_benchmarks.sh` | Copies `mgpusim/samples` into `private/ shared/ mgvm/ mgvm-nobalance/` |
| 3 | `3_gen_runners.py [--preset paper\|small]` | Writes `<config>/<workload>.sh` with the flags of section 3.1 and the input sizes |
| 4 | `4_run_benchmarks_<config>.sh` or `4_run_all.sh` | Runs the simulations; each writes `<config>/samples/<workload>/metrics.csv` |
| 5 | `5_collect_stats.py private shared mgvm-nobalance mgvm` | Parses all `metrics.csv` (via `helper_functions/`) into `results.csv` |
| 6 | `6_normalize_results.py results.csv normalized.csv` | Normalizes: throughput and walk latency to private, local/remote fractions |
| 7 | `7_plot_figures.py normalized.csv figures` | Draws Figures 7–10 (added by us) |

What each figure is computed from (`metrics.csv` → `results.csv` → `normalized.csv`):

| Figure | Metric | Source |
|---|---|---|
| 7 Throughput | `private_kernel_time / config_kernel_time` | `kernel_time` of the command processor |
| 8 L2 TLB hits | local hits / (local + remote hits) | `local-TLBHit-num-total`, `remote-TLBHit-num-total` (L1-miss tracer) |
| 9 PW accesses | local / (local + remote) PTE reads | `page_walk_req_local/remote` counted in `mmu.go` |
| 10 PW latency | `config / private` average walk latency | `mmu-pw-lat` |
| Table III | L2 TLB MPKI | `l2-tlb-mpki` in `results.csv` |

---

## 6. What was done in this repository (commit by commit)

1. **Imported the artifact unchanged** from the Zenodo archive (only removed a stray 20 MB binary,
   two prebuilt test binaries and Python caches). It builds as-is with Go 1.22/1.24.
2. **Made the scripts runnable on a normal machine**
   * `0_clean.sh` also removes the results; `2_copy_benchmarks.sh` can be re-run and run from any
     directory. (The original scripts work as they are; an earlier version of this document wrongly
     said `2_copy_benchmarks.sh` deleted its own folders.)
   * `3_gen_runners.py --preset small`: smaller inputs. The MGvm parameters are scaled with the
     rule that reproduces the authors' values for the dense-array workloads:
     `custom-hsl = largest allocation / 4 chiplets / 4 KB` (minimum 512 pages = 2 MB) and
     `hslaware-N` with `N = custom-hsl / 512`. For SPMV, MIS and SYR2K the authors used the 2 MB
     minimum, and so do we. GUPS, MT and S2D already used their paper inputs.
   * `4_run_all.sh`: runs all 60 simulations with a limit on parallel jobs (`MGVM_JOBS`), reports
     the real exit code (an earlier version logged out-of-memory kills as exit 0), and skips runs
     that already finished so it can be resumed.
   * `7_plot_figures.py` and a `Dockerfile` that builds from the repository.
3. **Memory limits.** With `-report-all` a simulation's memory grows with simulated time. On the
   16 GB container SPMV, SYRK and SYR2K were killed by the kernel's out-of-memory killer, so for the
   small preset SPMV was shrunk and SYRK/SYR2K were capped at 3 M / 10 M instructions
   (the paper caps them at 10 M / 30 M).
4. **Fixed a bug in `6_normalize_results.py`.** Its rows contained two separator columns before the
   L2-TLB-hit group and before the page-walk group, but the header has one, so every column from
   "L2 TLB Local Hits" onward was printed under the wrong header (Figures 8 and 9 would have been
   wrong). Throughput and walk latency were not affected.
5. **Ran all 60 simulations** (about 11 hours, 3 at a time) and committed `results/small/`.
6. **Paper-preset runs** of FW, MIS and SYRK were started afterwards (results added when done);
   GUPS, MT and S2D in the small preset already use the paper inputs.

---

## 7. Results (small preset) and how to read them

| Geomean over 15 workloads | Ours | Paper |
|---|---|---|
| MGvm vs. private TLB | 1.18x | 1.52x |
| MGvm vs. shared TLB | 1.19x | 1.30x |
| MGvm vs. better of private/shared, per workload | 1.06x | 1.12x |
| Page-walk latency, shared / MGvm (vs. private) | 1.31x / 0.75x | higher / lower |

Things that reproduce:

* **L2 TLB MPKI** under private TLB matches Table III almost exactly for workloads whose input is
  unchanged or that are not size-sensitive: C2D 1.07 (1.07), GUPS 699 (698), J1D 3.21 (3.21),
  J2D 2.16 (2.16), MT 69.3 (69.3), SC 0.41 (0.40).
* **Fig. 8**: the shared TLB serves only ~25 % of L2 TLB hits locally (1 of 4 chiplets is local);
  MGvm raises this to 97–100 % for C2D, FW, J1D, J2D, PR, SC and S2D.
* **Fig. 9**: with MGvm almost all PTE reads are local, except MIS and SYR2, which switched to the
  4 KB hash (dHSL-balance) and so gave up local PTE placement — the trade-off described in the paper.
* **Fig. 10**: shared TLB walks are slower than private; MGvm walks are faster.
* **Balancing** helps MIS (+33 %) and SYR2 (+29 %), the workloads the paper names.

Why the speedups are smaller: inputs are 4–64x smaller, so translation pressure is lower. C2D, J1D,
J2D and SC show almost no difference between configurations because translation is not their
bottleneck at this size; MIS is slower under MGvm than under private TLB at this size.

---

### Paper-size inputs (FW, MIS, SYRK)

These three were also run with the paper's inputs (`results/paper/`). L2 TLB MPKI matches Table III
almost exactly: FW 2.28 / 2.27 / 2.27 (paper 2.28 for all), MIS 261.2 / 2.18 / 8.66 (paper 260.5 /
2.11 / 8.50), SYRK 201.46 / 53.03 / 53.07 (paper 201.46 / 53.03 / 53.17), for private / shared / MGvm.

Throughput vs. private TLB: FW 0.98x / 1.00x / 1.00x (shared / nobalance / MGvm), MIS 5.02x / 4.10x /
4.96x, SYRK 1.96x / 1.75x / 1.97x. At paper size MIS shows the ~5x gain from TLB capacity that the
small preset missed, and balancing adds 21 %. MGvm keeps 89 % of MIS page-walk reads local (shared: 25 %).
SYRK switches to dHSL-balance and so its PTE reads are mostly remote, as the paper describes.
On MIS, MGvm only ties the shared TLB here, while the paper's Figure 7 shows MGvm ahead.

## 8. The other branch: `reimplementation-mgpusim-v3`

Before the artifact was available, MGvm was reimplemented from the paper on the current upstream
MGPUSim (v3, Akita v3): a `mgvm` package (HSL functions, L2 TLB slice, RTU, page walker with PWC,
page-table page placement, inter-chiplet network, runtime implementing Listings 1 and 2), driver
hooks for LASP-style placement, and the 8 workloads missing from upstream MGPUSim written in
OpenCL and compiled with stock clang-18. It is a cross-check only; all results above come from
the authors' artifact on `main`.

---

## 9. Questions mentors are likely to ask

* **Why not the paper's inputs?** The artifact states 300–400 GB of RAM per configuration and up to
  a day per run; the machine used had 16 GB and 4 cores. The `paper` preset is still the default of
  `3_gen_runners.py` for a bigger machine.
* **Is the small preset still meaningful?** The largest allocation is 8–16 MB for most workloads,
  above the 8 MB that all four L2 TLB slices together can map and far above one slice (2 MB), so
  the TLBs are still stressed; MPKIs match the paper for many workloads.
* **What is the single most important mechanism?** Making the HSL and the page-table placement
  agree with LASP's data placement: then a local data access is translated locally and walked
  locally (Figs. 8 and 9).
* **What was changed in the simulator itself?** Nothing. Only scripts, documentation and the
  normalization bug fix; the simulator code on `main` is the authors'.
