// Package gups implements the GUPS (random access) benchmark.
package gups

import (
	_ "embed"
	"log"

	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/common"
	"github.com/sarchlab/mgpusim/v3/driver"
	"github.com/sarchlab/mgpusim/v3/insts"
)

//go:embed kernels.hsaco
var hsacoBytes []byte

// KernelArgs defines the kernel arguments.
type KernelArgs struct {
	Table          driver.Ptr
	TableMask      uint32
	UpdatesPerItem uint32
	Seed           uint32
	Pad            uint32
}

// Benchmark is the GUPS benchmark.
type Benchmark struct {
	common.Base
	kernel *insts.HsaCo

	Log2TableSize  int // number of 4-byte table entries (power of 2)
	NumItems       int
	UpdatesPerItem int

	hOut []uint32
}

// NewBenchmark creates the benchmark.
func NewBenchmark(d *driver.Driver) *Benchmark {
	return &Benchmark{
		Base:           common.NewBase(d),
		kernel:         common.LoadKernel(hsacoBytes, "gups"),
		Log2TableSize:  22,
		NumItems:       16384,
		UpdatesPerItem: 4,
	}
}

const seed = 12345

// Run runs the benchmark.
func (b *Benchmark) Run() {
	b.Start()

	n := 1 << b.Log2TableSize
	b.hOut = make([]uint32, n)

	dTable := b.Alloc(uint64(n * 4))
	b.Driver.MemCopyH2D(b.Context, dTable, b.hOut)

	b.Launch(b.kernel,
		[3]uint32{uint32(common.RoundUp(b.NumItems, 256)), 1, 1},
		[3]uint16{256, 1, 1},
		&KernelArgs{dTable, uint32(n - 1), uint32(b.UpdatesPerItem), seed, 0})
	b.Wait()

	b.Driver.MemCopyD2H(b.Context, b.hOut, dTable)
}

// Verify compares the table with a sequential execution. GUPS tolerates a
// small fraction of lost updates caused by races, as in HPCC RandomAccess.
func (b *Benchmark) Verify() {
	n := 1 << b.Log2TableSize
	ref := make([]uint32, n)
	numItems := common.RoundUp(b.NumItems, 256)
	for g := 0; g < numItems; g++ {
		x := uint32(g)*2654435761 + seed
		for k := 0; k < b.UpdatesPerItem; k++ {
			x ^= x << 13
			x ^= x >> 17
			x ^= x << 5
			ref[x&uint32(n-1)] ^= x
		}
	}

	errors := 0
	for i := range ref {
		if ref[i] != b.hOut[i] {
			errors++
		}
	}
	if errors > numItems*b.UpdatesPerItem/100 {
		log.Panicf("%d table entries differ", errors)
	}
	log.Printf("Passed! (%d entries differ due to races)\n", errors)
}
