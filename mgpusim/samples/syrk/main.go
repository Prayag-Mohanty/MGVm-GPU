package main

import (
	"flag"

	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/syrk"
	"github.com/sarchlab/mgpusim/v3/samples/runner"
)

var n = flag.Int("n", 256, "number of rows of A (C is n x n)")
var m = flag.Int("m", 256, "number of columns of A")

func main() {
	flag.Parse()

	r := new(runner.Runner).Init()

	b := syrk.NewBenchmark(r.Driver())
	b.N = *n
	b.M = *m
	r.AddBenchmark(b)

	r.Run()
}
