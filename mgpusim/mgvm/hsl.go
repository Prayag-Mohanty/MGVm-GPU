// Package mgvm implements the virtual-memory subsystem of a multi-chip-module
// (MCM) GPU as described in "Designing Virtual Memory System of MCM GPUs"
// (MICRO 2022). It provides per-chiplet L2 TLB slices, page-table walkers with
// page-walk caches, Remote Translation Units (RTUs), the Home Slice Selection
// (HSL) function and the MGvm runtime that selects the HSL per kernel and
// switches between dHSL-coarse and dHSL-balance.
package mgvm

import "fmt"

// HSLKind is the type of the Home Slice Selection function.
type HSLKind int

const (
	// HSLPrivate always selects the requesting chiplet (private L2 TLB).
	HSLPrivate HSLKind = iota

	// HSLInterleave maps consecutive Granularity-sized VA chunks to
	// consecutive chiplets (shared L2 TLB, and dHSL-balance with 4KB).
	HSLInterleave

	// HSLCoarse is MGvm's dHSL-coarse. It maps Granularity-sized chunks,
	// counted from Base, to consecutive chiplets. Granularity is a multiple of
	// the VA range covered by one leaf page-table page (2MB with 4KB pages).
	HSLCoarse
)

// HSL is a Home Slice Selection function.
type HSL struct {
	Kind        HSLKind
	Granularity uint64
	Base        uint64
	NumChiplets int
}

// Home returns the chiplet whose L2 TLB slice (and page walkers) is
// responsible for translating vAddr when requested by chiplet requester.
func (h HSL) Home(vAddr uint64, requester int) int {
	switch h.Kind {
	case HSLPrivate:
		return requester
	case HSLInterleave:
		return int((vAddr / h.Granularity) % uint64(h.NumChiplets))
	case HSLCoarse:
		n := int64(h.NumChiplets)
		chunk := (int64(vAddr) - int64(h.Base)) / int64(h.Granularity)
		if int64(vAddr) < int64(h.Base) {
			chunk--
		}
		return int(((chunk % n) + n) % n)
	}

	panic("unknown HSL kind")
}

func (h HSL) String() string {
	switch h.Kind {
	case HSLPrivate:
		return "private"
	case HSLInterleave:
		return fmt.Sprintf("interleave(%dKB)", h.Granularity>>10)
	case HSLCoarse:
		return fmt.Sprintf("coarse(%dMB;base=0x%x)", h.Granularity>>20, h.Base)
	}

	return "unknown"
}

// HSLView is the copy of the HSL held by one hardware component (an RTU or an
// L2 TLB slice). Views are updated asynchronously when the runtime switches
// the HSL, so different components may temporarily disagree.
type HSLView struct {
	hsl HSL
}

// Get returns the HSL currently used by the component.
func (v *HSLView) Get() HSL {
	return v.hsl
}

// Set updates the HSL used by the component.
func (v *HSLView) Set(h HSL) {
	v.hsl = h
}
