// Package mis implements the maximal independent set benchmark (MIS) from
// Pannotia on a synthetic graph.
package mis

import (
	_ "embed"
	"log"
	"math/rand"

	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/common"
	"github.com/sarchlab/mgpusim/v3/driver"
	"github.com/sarchlab/mgpusim/v3/insts"
)

//go:embed kernels.hsaco
var hsacoBytes []byte

// SelectArgs are the arguments of mis_select.
type SelectArgs struct {
	RowPtr, Cols, Value, Status, Selected, Undecided driver.Ptr
	NumNodes                                         int32
	Pad                                              int32
}

// UpdateArgs are the arguments of mis_update.
type UpdateArgs struct {
	RowPtr, Cols, Selected, Status driver.Ptr
	NumNodes                       int32
	Pad                            int32
}

// Benchmark is the MIS benchmark.
type Benchmark struct {
	common.Base
	selectKernel, updateKernel *insts.HsaCo

	NumNodes  int
	AvgDegree int
	// LocalFraction is the fraction of edges connecting nodes whose IDs are
	// close, the rest connect random nodes (irregular accesses).
	LocalFraction float64
	MaxIters      int

	rowPtr, cols []int32
	status       []int32
}

// NewBenchmark creates the benchmark.
func NewBenchmark(d *driver.Driver) *Benchmark {
	return &Benchmark{
		Base:          common.NewBase(d),
		selectKernel:  common.LoadKernel(hsacoBytes, "mis_select"),
		updateKernel:  common.LoadKernel(hsacoBytes, "mis_update"),
		NumNodes:      1 << 18,
		AvgDegree:     8,
		LocalFraction: 0.75,
		MaxIters:      1 << 20,
	}
}

func (b *Benchmark) buildGraph() {
	r := rand.New(rand.NewSource(1))
	adj := make([][]int32, b.NumNodes)
	numUndirected := b.NumNodes * b.AvgDegree / 2
	for e := 0; e < numUndirected; e++ {
		u := r.Intn(b.NumNodes)
		var v int
		if r.Float64() < b.LocalFraction {
			v = u + r.Intn(64) - 32
			if v < 0 || v >= b.NumNodes {
				v = r.Intn(b.NumNodes)
			}
		} else {
			v = r.Intn(b.NumNodes)
		}
		if u == v {
			continue
		}
		adj[u] = append(adj[u], int32(v))
		adj[v] = append(adj[v], int32(u))
	}

	b.rowPtr = make([]int32, b.NumNodes+1)
	for v := 0; v < b.NumNodes; v++ {
		b.rowPtr[v+1] = b.rowPtr[v] + int32(len(adj[v]))
		b.cols = append(b.cols, adj[v]...)
	}
}

// Run runs the benchmark.
func (b *Benchmark) Run() {
	b.Start()
	b.buildGraph()

	n := b.NumNodes
	r := rand.New(rand.NewSource(2))
	value := make([]uint32, n)
	for i := range value {
		value[i] = r.Uint32()
	}
	b.status = make([]int32, n)
	for i := range b.status {
		b.status[i] = -1
	}

	dCols := b.Alloc(uint64(len(b.cols) * 4))
	dRowPtr := b.Alloc(uint64((n + 1) * 4))
	dValue := b.Alloc(uint64(n * 4))
	dStatus := b.Alloc(uint64(n * 4))
	dSelected := b.Alloc(uint64(n * 4))
	dUndecided := b.Alloc(4)
	b.Driver.MemCopyH2D(b.Context, dCols, b.cols)
	b.Driver.MemCopyH2D(b.Context, dRowPtr, b.rowPtr)
	b.Driver.MemCopyH2D(b.Context, dValue, value)
	b.Driver.MemCopyH2D(b.Context, dStatus, b.status)

	grid := [3]uint32{uint32(common.RoundUp(n, 256)), 1, 1}
	wg := [3]uint16{256, 1, 1}
	undecided := []int32{0}
	for iter := 0; iter < b.MaxIters; iter++ {
		undecided[0] = 0
		b.Driver.MemCopyH2D(b.Context, dUndecided, undecided)

		b.Launch(b.selectKernel, grid, wg, &SelectArgs{
			dRowPtr, dCols, dValue, dStatus, dSelected, dUndecided, int32(n), 0})
		b.Launch(b.updateKernel, grid, wg, &UpdateArgs{
			dRowPtr, dCols, dSelected, dStatus, int32(n), 0})
		b.Wait()

		b.Driver.MemCopyD2H(b.Context, undecided, dUndecided)
		if undecided[0] == 0 {
			break
		}
	}

	b.Driver.MemCopyD2H(b.Context, b.status, dStatus)
}

// Verify checks that the result is a maximal independent set.
func (b *Benchmark) Verify() {
	for v := 0; v < b.NumNodes; v++ {
		hasSetNeighbor := false
		for e := b.rowPtr[v]; e < b.rowPtr[v+1]; e++ {
			u := b.cols[e]
			if b.status[u] == 1 {
				hasSetNeighbor = true
			}
		}

		switch b.status[v] {
		case 1:
			if hasSetNeighbor {
				log.Panicf("node %d and a neighbor are both in the set", v)
			}
		case 0:
			if !hasSetNeighbor {
				log.Panicf("excluded node %d has no neighbor in the set", v)
			}
		default:
			if b.MaxIters >= 1<<20 {
				log.Panicf("node %d is undecided", v)
			}
		}
	}
	log.Printf("Passed!\n")
}
