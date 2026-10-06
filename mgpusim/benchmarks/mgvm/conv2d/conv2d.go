// Package conv2d implements the PolyBench 2DCONV benchmark (C2D).
package conv2d

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
	A, B   driver.Ptr
	NI, NJ int32
}

// Benchmark is the 2D convolution benchmark.
type Benchmark struct {
	common.Base
	kernel *insts.HsaCo

	NI, NJ int

	hA, hB []float32
}

// NewBenchmark creates the benchmark.
func NewBenchmark(d *driver.Driver) *Benchmark {
	return &Benchmark{
		Base:   common.NewBase(d),
		kernel: common.LoadKernel(hsacoBytes, "conv2d"),
		NI:     1024,
		NJ:     1024,
	}
}

// Run runs the benchmark.
func (b *Benchmark) Run() {
	b.Start()

	n := b.NI * b.NJ
	b.hA = make([]float32, n)
	for i := range b.hA {
		b.hA[i] = float32(i%1013) / 1013
	}
	b.hB = make([]float32, n)

	dA := b.Alloc(uint64(n * 4))
	dB := b.Alloc(uint64(n * 4))
	b.Driver.MemCopyH2D(b.Context, dA, b.hA)
	b.Driver.MemCopyH2D(b.Context, dB, b.hB)

	b.Launch(b.kernel,
		[3]uint32{uint32(common.RoundUp(b.NJ, 16)), uint32(common.RoundUp(b.NI, 16)), 1},
		[3]uint16{16, 16, 1},
		&KernelArgs{dA, dB, int32(b.NI), int32(b.NJ)})
	b.Wait()

	b.Driver.MemCopyD2H(b.Context, b.hB, dB)
}

// Verify checks the result.
func (b *Benchmark) Verify() {
	NJ := b.NJ
	A := b.hA
	for i := 1; i < b.NI-1; i++ {
		for j := 1; j < NJ-1; j++ {
			e := 0.2*A[(i-1)*NJ+(j-1)] + -0.3*A[i*NJ+(j-1)] + 0.4*A[(i+1)*NJ+(j-1)] +
				0.5*A[(i-1)*NJ+j] + 0.6*A[i*NJ+j] + 0.7*A[(i+1)*NJ+j] +
				-0.8*A[(i-1)*NJ+(j+1)] + -0.9*A[i*NJ+(j+1)] + 0.10*A[(i+1)*NJ+(j+1)]
			if math.Abs(float64(e-b.hB[i*NJ+j])) > 1e-4 {
				log.Panicf("mismatch at (%d,%d): expected %f, got %f",
					i, j, e, b.hB[i*NJ+j])
			}
		}
	}
	log.Printf("Passed!\n")
}
