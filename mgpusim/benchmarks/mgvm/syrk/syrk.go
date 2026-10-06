// Package syrk implements the PolyBench SYRK benchmark (symmetric rank-k
// update) used in the MGvm evaluation.
package syrk

import (
	_ "embed"
	"log"
	"math"

	"github.com/sarchlab/mgpusim/v3/driver"
	"github.com/sarchlab/mgpusim/v3/insts"
	"github.com/sarchlab/mgpusim/v3/kernels"
)

//go:embed kernels.hsaco
var hsacoBytes []byte

// KernelArgs defines the kernel arguments.
type KernelArgs struct {
	A     driver.Ptr
	C     driver.Ptr
	Alpha float32
	Beta  float32
	N     int32
	M     int32
}

// Benchmark defines the SYRK benchmark.
type Benchmark struct {
	driver  *driver.Driver
	context *driver.Context
	gpus    []int
	queue   *driver.CommandQueue
	kernel  *insts.HsaCo

	N, M int

	hA, hC, hOut []float32
	dA, dC       driver.Ptr
}

// NewBenchmark creates a new benchmark.
func NewBenchmark(d *driver.Driver) *Benchmark {
	b := &Benchmark{driver: d, N: 256, M: 256}
	b.context = d.Init()
	b.kernel = kernels.LoadProgramFromMemoryCOV4(hsacoBytes, "syrk")
	if b.kernel == nil {
		log.Panic("failed to load kernel")
	}
	return b
}

// SelectGPU selects the GPUs to run on.
func (b *Benchmark) SelectGPU(gpus []int) { b.gpus = gpus }

// SetUnifiedMemory is not supported.
func (b *Benchmark) SetUnifiedMemory() { panic("unified memory not supported") }

// Run runs the benchmark.
func (b *Benchmark) Run() {
	b.driver.SelectGPU(b.context, b.gpus[0])
	b.queue = b.driver.CreateCommandQueue(b.context)

	b.hA = make([]float32, b.N*b.M)
	b.hC = make([]float32, b.N*b.N)
	b.hOut = make([]float32, b.N*b.N)
	for i := range b.hA {
		b.hA[i] = float32(i%97) / 97
	}
	for i := range b.hC {
		b.hC[i] = float32(i%89) / 89
	}

	// The largest allocation (C) is allocated first.
	b.dC = b.driver.AllocateMemory(b.context, uint64(len(b.hC)*4))
	b.dA = b.driver.AllocateMemory(b.context, uint64(len(b.hA)*4))
	b.driver.MemCopyH2D(b.context, b.dA, b.hA)
	b.driver.MemCopyH2D(b.context, b.dC, b.hC)

	args := KernelArgs{b.dA, b.dC, 1.5, 1.2, int32(b.N), int32(b.M)}
	b.driver.EnqueueLaunchKernel(b.queue, b.kernel,
		[3]uint32{uint32(b.N), uint32(b.N), 1},
		[3]uint16{16, 16, 1}, &args)
	b.driver.DrainCommandQueue(b.queue)

	b.driver.MemCopyD2H(b.context, b.hOut, b.dC)
}

// Verify checks the result against a CPU implementation.
func (b *Benchmark) Verify() {
	for i := 0; i < b.N; i++ {
		for j := 0; j < b.N; j++ {
			acc := b.hC[i*b.N+j] * 1.2
			for k := 0; k < b.M; k++ {
				acc += 1.5 * b.hA[i*b.M+k] * b.hA[j*b.M+k]
			}
			got := b.hOut[i*b.N+j]
			if math.Abs(float64(got-acc)) > 1e-2*math.Max(1, math.Abs(float64(acc))) {
				log.Panicf("mismatch at (%d,%d): expected %f, got %f", i, j, acc, got)
			}
		}
	}
	log.Printf("Passed!\n")
}
