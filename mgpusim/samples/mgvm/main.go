// Command mgvm runs the 15 workloads of the MGvm evaluation (Table II of
// "Designing Virtual Memory System of MCM GPUs", MICRO 2022) on the
// simulated MCM GPU.
//
// Example:
//
//	mgvm -bench=SYRK -vm=mgvm -report-all
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/sarchlab/mgpusim/v3/benchmarks"
	"github.com/sarchlab/mgpusim/v3/benchmarks/amdappsdk/fastwalshtransform"
	"github.com/sarchlab/mgpusim/v3/benchmarks/amdappsdk/matrixtranspose"
	"github.com/sarchlab/mgpusim/v3/benchmarks/amdappsdk/simpleconvolution"
	"github.com/sarchlab/mgpusim/v3/benchmarks/heteromark/kmeans"
	"github.com/sarchlab/mgpusim/v3/benchmarks/heteromark/pagerank"
	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/conv2d"
	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/gups"
	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/jacobi1d"
	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/jacobi2d"
	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/mis"
	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/reduction"
	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/syr2k"
	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/syrk"
	"github.com/sarchlab/mgpusim/v3/benchmarks/shoc/spmv"
	"github.com/sarchlab/mgpusim/v3/benchmarks/shoc/stencil2d"
	"github.com/sarchlab/mgpusim/v3/driver"
	"github.com/sarchlab/mgpusim/v3/samples/runner"
)

var benchFlag = flag.String("bench", "", "Workload to run (see -list).")
var listFlag = flag.Bool("list", false, "List the workloads and exit.")
var nFlag = flag.Int("n", 0, "Primary problem size (0: workload default).")
var mFlag = flag.Int("m", 0, "Secondary problem size (0: workload default).")
var itersFlag = flag.Int("iters", 0, "Iterations/time steps (0: default).")

type workload struct {
	desc     string
	locality string
	n, m, it int
	build    func(d *driver.Driver, n, m, it int) benchmarks.Benchmark
}

// Default sizes are scaled down from Table II so that the timing simulation
// of all workloads and configurations completes on a workstation. They still
// exceed the aggregate L2 TLB reach (4 x 512 x 4KB = 8MB) where the paper's
// footprints do.
var workloads = map[string]workload{
	"C2D": {"2-D convolution (PolyBench)", "NL", 2048, 2048, 0,
		func(d *driver.Driver, n, m, _ int) benchmarks.Benchmark {
			b := conv2d.NewBenchmark(d)
			b.NI, b.NJ = n, m
			return b
		}},
	"FW": {"fast Walsh transform (AMD APP SDK)", "RCL", 1 << 21, 0, 0,
		func(d *driver.Driver, n, _, _ int) benchmarks.Benchmark {
			b := fastwalshtransform.NewBenchmark(d)
			b.Length = uint32(n)
			return b
		}},
	"GUPS": {"multi-threaded random access", "unclassified", 22, 16384, 4,
		func(d *driver.Driver, n, m, it int) benchmarks.Benchmark {
			b := gups.NewBenchmark(d)
			b.Log2TableSize, b.NumItems, b.UpdatesPerItem = n, m, it
			return b
		}},
	"J1D": {"1-D Jacobi solver (PolyBench)", "NL", 1 << 22, 0, 1,
		func(d *driver.Driver, n, _, it int) benchmarks.Benchmark {
			b := jacobi1d.NewBenchmark(d)
			b.N, b.Steps = n, it
			return b
		}},
	"J2D": {"2-D Jacobi solver (PolyBench)", "NL", 2048, 0, 1,
		func(d *driver.Driver, n, _, it int) benchmarks.Benchmark {
			b := jacobi2d.NewBenchmark(d)
			b.N, b.Steps = n, it
			return b
		}},
	"KM": {"k-means clustering, 20 clusters (Hetero-Mark)", "ITL", 65536, 32, 1,
		func(d *driver.Driver, n, m, it int) benchmarks.Benchmark {
			b := kmeans.NewBenchmark(d)
			b.NumPoints, b.NumFeatures, b.NumClusters, b.MaxIter = n, m, 20, it
			return b
		}},
	"MIS": {"maximal independent set (Pannotia-style)", "NL+ITL", 1 << 18, 8, 0,
		func(d *driver.Driver, n, m, _ int) benchmarks.Benchmark {
			b := mis.NewBenchmark(d)
			b.NumNodes, b.AvgDegree = n, m
			return b
		}},
	"MT": {"matrix transpose (AMD APP SDK)", "RCL", 2048, 0, 0,
		func(d *driver.Driver, n, _, _ int) benchmarks.Benchmark {
			b := matrixtranspose.NewBenchmark(d)
			b.Width = n
			return b
		}},
	"PR": {"PageRank (Hetero-Mark)", "ITL", 65536, 16, 2,
		func(d *driver.Driver, n, m, it int) benchmarks.Benchmark {
			b := pagerank.NewBenchmark(d)
			b.NumNodes = uint32(n)
			b.NumConnections = uint32(n * m)
			b.MaxIterations = uint32(it)
			return b
		}},
	"SC": {"simple convolution (AMD APP SDK)", "NL", 2046, 2046, 0,
		func(d *driver.Driver, n, m, _ int) benchmarks.Benchmark {
			b := simpleconvolution.NewBenchmark(d)
			b.Width, b.Height = uint32(n), uint32(m)
			b.SetMaskSize(3)
			return b
		}},
	"RED": {"reduction (SHOC)", "NL", 1 << 23, 1024, 0,
		func(d *driver.Driver, n, m, _ int) benchmarks.Benchmark {
			b := reduction.NewBenchmark(d)
			b.N, b.NumGroups = n, m
			return b
		}},
	"SPMV": {"sparse matrix-vector multiplication (SHOC)", "ITL", 32768, 64, 0,
		func(d *driver.Driver, n, m, _ int) benchmarks.Benchmark {
			b := spmv.NewBenchmark(d)
			b.Dim = int32(n)
			b.Sparsity = float64(m) / float64(n)
			return b
		}},
	"S2D": {"2-D stencil (SHOC)", "NL", 1024, 1024, 1,
		func(d *driver.Driver, n, m, it int) benchmarks.Benchmark {
			b := stencil2d.NewBenchmark(d)
			b.NumRows, b.NumCols, b.NumIteration = n+2, m+2, it
			return b
		}},
	"SYRK": {"symmetric rank-k update (PolyBench)", "RCL", 1024, 64, 0,
		func(d *driver.Driver, n, m, _ int) benchmarks.Benchmark {
			b := syrk.NewBenchmark(d)
			b.N, b.M = n, m
			return b
		}},
	"SYR2": {"symmetric rank-2k update (PolyBench)", "RCL", 1024, 32, 0,
		func(d *driver.Driver, n, m, _ int) benchmarks.Benchmark {
			b := syr2k.NewBenchmark(d)
			b.N, b.M = n, m
			return b
		}},
}

func pick(v, def int) int {
	if v != 0 {
		return v
	}
	return def
}

func main() {
	flag.Parse()

	if *listFlag {
		names := make([]string, 0, len(workloads))
		for k := range workloads {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			w := workloads[k]
			fmt.Printf("%-5s %-14s n=%d m=%d iters=%d  %s\n",
				k, w.locality, w.n, w.m, w.it, w.desc)
		}
		return
	}

	w, ok := workloads[strings.ToUpper(*benchFlag)]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown workload %q, use -list\n", *benchFlag)
		os.Exit(1)
	}

	r := new(runner.Runner).Init()

	b := w.build(r.Driver(), pick(*nFlag, w.n), pick(*mFlag, w.m),
		pick(*itersFlag, w.it))
	log.Printf("running %s (%s)", *benchFlag, w.desc)
	r.AddBenchmark(b)

	r.Run()
}
