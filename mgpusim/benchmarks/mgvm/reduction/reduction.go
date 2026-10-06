// Package reduction implements the SHOC reduction benchmark (RED).
package reduction

import (
	_ "embed"
	"log"
	"math"

	"github.com/sarchlab/mgpusim/v3/benchmarks/mgvm/common"
	"github.com/sarchlab/mgpusim/v3/driver"
	"github.com/sarchlab/mgpusim/v3/insts"
)

//go:embed kernels.hsaco
var hsacoBytes []byte

// KernelArgs defines the kernel arguments.
type KernelArgs struct {
	In, Partial   driver.Ptr
	ElemsPerGroup uint32
	Pad           uint32
}

// Benchmark is the reduction benchmark.
type Benchmark struct {
	common.Base
	kernel *insts.HsaCo

	N         int // number of float elements
	NumGroups int

	hIn      []float32
	hPartial []float32
}

// NewBenchmark creates the benchmark.
func NewBenchmark(d *driver.Driver) *Benchmark {
	return &Benchmark{
		Base:      common.NewBase(d),
		kernel:    common.LoadKernel(hsacoBytes, "reduce"),
		N:         1 << 22,
		NumGroups: 512,
	}
}

// Run runs the benchmark.
func (b *Benchmark) Run() {
	b.Start()

	if b.N%b.NumGroups != 0 {
		log.Panic("N must be a multiple of the number of work-groups")
	}

	b.hIn = make([]float32, b.N)
	for i := range b.hIn {
		b.hIn[i] = float32(i%7) - 3
	}
	b.hPartial = make([]float32, b.NumGroups)

	dIn := b.Alloc(uint64(b.N * 4))
	dPartial := b.Alloc(uint64(b.NumGroups * 4))
	b.Driver.MemCopyH2D(b.Context, dIn, b.hIn)

	b.Launch(b.kernel,
		[3]uint32{uint32(b.NumGroups * 256), 1, 1},
		[3]uint16{256, 1, 1},
		&KernelArgs{dIn, dPartial, uint32(b.N / b.NumGroups), 0})
	b.Wait()

	b.Driver.MemCopyD2H(b.Context, b.hPartial, dPartial)
}

// Verify checks the partial sums.
func (b *Benchmark) Verify() {
	per := b.N / b.NumGroups
	for g := 0; g < b.NumGroups; g++ {
		sum := 0.0
		for i := g * per; i < (g+1)*per; i++ {
			sum += float64(b.hIn[i])
		}
		if math.Abs(sum-float64(b.hPartial[g])) > 1e-2 {
			log.Panicf("mismatch at group %d: expected %f, got %f",
				g, sum, b.hPartial[g])
		}
	}
	log.Printf("Passed!\n")
}
