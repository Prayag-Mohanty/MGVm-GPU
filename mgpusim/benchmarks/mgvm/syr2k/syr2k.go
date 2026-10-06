// Package syr2k implements the PolyBench SYR2K benchmark (SYR2).
package syr2k

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
	A, B, C     driver.Ptr
	Alpha, Beta float32
	N, M        int32
}

// Benchmark is the SYR2K benchmark.
type Benchmark struct {
	common.Base
	kernel *insts.HsaCo

	N, M int

	hA, hB, hC, hOut []float32
}

// NewBenchmark creates the benchmark.
func NewBenchmark(d *driver.Driver) *Benchmark {
	return &Benchmark{
		Base:   common.NewBase(d),
		kernel: common.LoadKernel(hsacoBytes, "syr2k"),
		N:      512,
		M:      64,
	}
}

// Run runs the benchmark.
func (b *Benchmark) Run() {
	b.Start()

	b.hA = make([]float32, b.N*b.M)
	b.hB = make([]float32, b.N*b.M)
	b.hC = make([]float32, b.N*b.N)
	b.hOut = make([]float32, b.N*b.N)
	for i := range b.hA {
		b.hA[i] = float32(i%97) / 97
		b.hB[i] = float32(i%83) / 83
	}
	for i := range b.hC {
		b.hC[i] = float32(i%89) / 89
	}

	dC := b.Alloc(uint64(len(b.hC) * 4))
	dA := b.Alloc(uint64(len(b.hA) * 4))
	dB := b.Alloc(uint64(len(b.hB) * 4))
	b.Driver.MemCopyH2D(b.Context, dA, b.hA)
	b.Driver.MemCopyH2D(b.Context, dB, b.hB)
	b.Driver.MemCopyH2D(b.Context, dC, b.hC)

	g := uint32(common.RoundUp(b.N, 16))
	b.Launch(b.kernel, [3]uint32{g, g, 1}, [3]uint16{16, 16, 1},
		&KernelArgs{dA, dB, dC, 1.5, 1.2, int32(b.N), int32(b.M)})
	b.Wait()

	b.Driver.MemCopyD2H(b.Context, b.hOut, dC)
}

// Verify checks the result.
func (b *Benchmark) Verify() {
	N, M := b.N, b.M
	for i := 0; i < N; i++ {
		for j := 0; j < N; j++ {
			acc := b.hC[i*N+j] * 1.2
			for k := 0; k < M; k++ {
				acc += 1.5*b.hA[i*M+k]*b.hB[j*M+k] + 1.5*b.hB[i*M+k]*b.hA[j*M+k]
			}
			got := b.hOut[i*N+j]
			if math.Abs(float64(got-acc)) > 1e-2*math.Max(1, math.Abs(float64(acc))) {
				log.Panicf("mismatch at (%d,%d): expected %f, got %f", i, j, acc, got)
			}
		}
	}
	log.Printf("Passed!\n")
}
