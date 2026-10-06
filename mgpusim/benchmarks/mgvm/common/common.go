// Package common provides host-side helpers shared by the MGvm benchmarks.
package common

import (
	"log"

	"github.com/sarchlab/mgpusim/v3/driver"
	"github.com/sarchlab/mgpusim/v3/insts"
	"github.com/sarchlab/mgpusim/v3/kernels"
)

// Base holds the driver state used by a benchmark.
type Base struct {
	Driver  *driver.Driver
	Context *driver.Context
	GPUs    []int
	Queue   *driver.CommandQueue
}

// NewBase creates a Base.
func NewBase(d *driver.Driver) Base {
	return Base{Driver: d, Context: d.Init()}
}

// SelectGPU selects the GPUs to run on. MGvm benchmarks run on one (possibly
// unified multi-chiplet) GPU.
func (b *Base) SelectGPU(gpus []int) {
	if len(gpus) != 1 {
		log.Panic("MGvm benchmarks run on a single (unified) GPU; " +
			"use -unified-gpus or -vm")
	}
	b.GPUs = gpus
}

// SetUnifiedMemory is not supported.
func (b *Base) SetUnifiedMemory() {
	log.Panic("unified memory is not supported by the MGvm benchmarks")
}

// Start selects the GPU and creates the command queue.
func (b *Base) Start() {
	b.Driver.SelectGPU(b.Context, b.GPUs[0])
	b.Queue = b.Driver.CreateCommandQueue(b.Context)
}

// LoadKernel loads a kernel compiled with clang (code object v4).
func LoadKernel(hsaco []byte, name string) *insts.HsaCo {
	k := kernels.LoadProgramFromMemoryCOV4(hsaco, name)
	if k == nil {
		log.Panicf("failed to load kernel %s", name)
	}
	return k
}

// Alloc allocates byteSize bytes of GPU memory.
func (b *Base) Alloc(byteSize uint64) driver.Ptr {
	return b.Driver.AllocateMemory(b.Context, byteSize)
}

// Launch enqueues a kernel launch.
func (b *Base) Launch(
	k *insts.HsaCo,
	grid [3]uint32,
	wg [3]uint16,
	args interface{},
) {
	b.Driver.EnqueueLaunchKernel(b.Queue, k, grid, wg, args)
}

// Wait waits for all the enqueued commands to finish.
func (b *Base) Wait() {
	b.Driver.DrainCommandQueue(b.Queue)
}

// RoundUp rounds n up to a multiple of m.
func RoundUp(n, m int) int {
	return (n + m - 1) / m * m
}
