package driver

import (
	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
	"github.com/sarchlab/mgpusim/v3/driver/internal"
)

// CTAPolicy decides how the work-groups of a kernel launched on a unified
// (multi-chiplet) GPU are assigned to the chiplets.
type CTAPolicy int

const (
	// CTAPolicyMGPUSimDefault keeps MGPUSim's original distribution.
	CTAPolicyMGPUSimDefault CTAPolicy = iota

	// CTAPolicyContiguous splits the flattened work-group ID space into
	// numChiplets equally-sized contiguous ranges (LASP-style scheduling that
	// matches PlacementLASPBlock data placement).
	CTAPolicyContiguous

	// CTAPolicyRoundRobin assigns work-group i to chiplet i % numChiplets
	// (the "naive" baseline).
	CTAPolicyRoundRobin
)

// Allocation describes a live memory allocation of a context.
type Allocation struct {
	VAddr uint64
	Size  uint64
}

// KernelLaunchHook is invoked by the driver right before a kernel is
// dispatched to a unified GPU. It is where the MGvm driver extension decides
// the HSL and places page-table pages (Listing 1 of the MGvm paper).
type KernelLaunchHook func(now sim.VTimeInSec, pid vm.PID, allocs []Allocation)

// PagePlacement selects the data placement for unified-GPU allocations.
type PagePlacement = internal.PlacementPolicy

// Page placement policies.
const (
	PlacementRoundRobin = internal.PlacementRoundRobin
	PlacementLASPBlock  = internal.PlacementLASPBlock
)

// SetUnifiedGPUPlacement selects the data placement policy and the virtual
// address alignment of every allocation made on a unified GPU.
func (d *Driver) SetUnifiedGPUPlacement(p PagePlacement, vaAlignment uint64) {
	d.memAllocator.SetUnifiedGPUPlacement(p, vaAlignment)
}

// SetPagePlacedHook registers a callback invoked whenever a page is mapped.
func (d *Driver) SetPagePlacedHook(hook func(page vm.Page)) {
	d.memAllocator.SetPagePlacedHook(hook)
}

// SetCTAPolicy sets how work-groups are assigned to chiplets.
func (d *Driver) SetCTAPolicy(p CTAPolicy) {
	d.ctaPolicy = p
}

// SetKernelLaunchHook registers the per-kernel-launch hook.
func (d *Driver) SetKernelLaunchHook(h KernelLaunchHook) {
	d.kernelLaunchHook = h
}

// Allocations returns the live allocations made in the context.
func (c *Context) Allocations() []Allocation {
	allocs := make([]Allocation, 0, len(c.buffers))
	for _, b := range c.buffers {
		if b.freed {
			continue
		}
		allocs = append(allocs, Allocation{VAddr: uint64(b.vAddr), Size: b.size})
	}

	return allocs
}

// PID returns the process ID of the context.
func (c *Context) PID() vm.PID {
	return c.pid
}
