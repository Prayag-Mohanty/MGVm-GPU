// Package jacobi2d implements the PolyBench JACOBI-2D benchmark (J2D).
package jacobi2d

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
	A, B driver.Ptr
	N    int32
	Pad  int32
}

// Benchmark is the Jacobi-2D benchmark.
type Benchmark struct {
	common.Base
	kernel *insts.HsaCo

	N     int
	Steps int

	hA, hOut []float32
}

// NewBenchmark creates the benchmark.
func NewBenchmark(d *driver.Driver) *Benchmark {
	return &Benchmark{
		Base:   common.NewBase(d),
		kernel: common.LoadKernel(hsacoBytes, "jacobi2d"),
		N:      1024,
		Steps:  1,
	}
}

// Run runs the benchmark.
func (b *Benchmark) Run() {
	b.Start()

	n := b.N * b.N
	b.hA = make([]float32, n)
	for i := range b.hA {
		b.hA[i] = float32(i%1021) / 1021
	}
	b.hOut = make([]float32, n)

	dA := b.Alloc(uint64(n * 4))
	dB := b.Alloc(uint64(n * 4))
	b.Driver.MemCopyH2D(b.Context, dA, b.hA)
	b.Driver.MemCopyH2D(b.Context, dB, b.hA)

	g := uint32(common.RoundUp(b.N, 16))
	grid := [3]uint32{g, g, 1}
	wg := [3]uint16{16, 16, 1}
	for t := 0; t < b.Steps; t++ {
		b.Launch(b.kernel, grid, wg, &KernelArgs{dA, dB, int32(b.N), 0})
		b.Launch(b.kernel, grid, wg, &KernelArgs{dB, dA, int32(b.N), 0})
	}
	b.Wait()

	b.Driver.MemCopyD2H(b.Context, b.hOut, dA)
}

// Verify checks the result.
func (b *Benchmark) Verify() {
	N := b.N
	a := append([]float32(nil), b.hA...)
	c := append([]float32(nil), b.hA...)
	for t := 0; t < 2*b.Steps; t++ {
		for i := 1; i < N-1; i++ {
			for j := 1; j < N-1; j++ {
				c[i*N+j] = 0.2 * (a[i*N+j] + a[i*N+j-1] + a[i*N+j+1] +
					a[(i+1)*N+j] + a[(i-1)*N+j])
			}
		}
		a, c = c, a
	}
	for i := range a {
		if math.Abs(float64(a[i]-b.hOut[i])) > 1e-4 {
			log.Panicf("mismatch at %d: expected %f, got %f", i, a[i], b.hOut[i])
		}
	}
	log.Printf("Passed!\n")
}
