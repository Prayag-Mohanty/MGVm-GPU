package mgvm

import (
	"sync"

	"github.com/sarchlab/akita/v3/mem/vm"
)

// Radix-tree geometry: a 4-level, x86-64 style page table with 4KB
// page-table pages holding 512 8-byte PTEs each. Level 0 is the root, level 3
// holds the leaf PTEs. A leaf page-table page covers 2MB of virtual address
// space.
const (
	NumPTLevels      = 4
	ptBitsPerLevel   = 9
	ptPageSize       = 4096
	pteSize          = 8
	LeafCoverageLog2 = 21 // one leaf page-table page maps 2MB
	vaBits           = 48
)

// ptPagePrefix returns the identifier of the level-l page-table page that is
// on the walk path of vAddr.
func ptPagePrefix(vAddr uint64, level int) uint64 {
	return vAddr >> (vaBits - ptBitsPerLevel*uint(level))
}

// pteIndex returns the index of the PTE in the level-l page for vAddr.
func pteIndex(vAddr uint64, level int) uint64 {
	shift := vaBits - ptBitsPerLevel*uint(level+1)
	return (vAddr >> shift) & ((1 << ptBitsPerLevel) - 1)
}

type ptKey struct {
	pid    vm.PID
	level  int
	prefix uint64
}

type ptFrameKey struct {
	key     ptKey
	chiplet int
}

// PTPlacer decides on which chiplet's memory each page-table page lives and
// provides the physical addresses of PTEs to the page walkers. The functional
// translation is still done with MGPUSim's page table; the PTPlacer only
// models where the radix-tree pages reside so that page walks issue memory
// accesses to the right (local or remote) memory.
type PTPlacer struct {
	sync.Mutex

	numChiplets int
	regionBase  []uint64
	regionSize  uint64
	nextFrame   []uint64

	placement map[ptKey]int
	frames    map[ptFrameKey]uint64
}

// NewPTPlacer creates a PTPlacer. regionBase[c] is the physical address of
// the memory reserved for page-table pages on chiplet c.
func NewPTPlacer(regionBase []uint64, regionSize uint64) *PTPlacer {
	return &PTPlacer{
		numChiplets: len(regionBase),
		regionBase:  regionBase,
		regionSize:  regionSize,
		nextFrame:   make([]uint64, len(regionBase)),
		placement:   make(map[ptKey]int),
		frames:      make(map[ptFrameKey]uint64),
	}
}

// OnDataPagePlaced is called when the driver maps a data page. Page-table
// pages on the walk path that are not placed yet are placed on the chiplet
// that holds the data page (first-placement policy, the baseline policy of
// the paper, similar to Linux on multi-socket machines).
func (p *PTPlacer) OnDataPagePlaced(pid vm.PID, vAddr uint64, chiplet int) {
	p.Lock()
	defer p.Unlock()

	for l := 0; l < NumPTLevels; l++ {
		k := ptKey{pid, l, ptPagePrefix(vAddr, l)}
		if _, ok := p.placement[k]; !ok {
			p.placement[k] = chiplet
		}
	}
}

// PlaceLeafPagesByHSL moves every leaf page-table page to the chiplet that
// the HSL selects for the 2MB VA region the page maps (MGvm step 2).
func (p *PTPlacer) PlaceLeafPagesByHSL(hsl HSL) {
	p.Lock()
	defer p.Unlock()

	for k := range p.placement {
		if k.level != NumPTLevels-1 {
			continue
		}

		regionStart := k.prefix << LeafCoverageLog2
		p.placement[k] = hsl.Home(regionStart, 0)
	}
}

// PTEAddr returns the physical address of the level-l PTE used to translate
// vAddr and the chiplet whose memory holds it.
func (p *PTPlacer) PTEAddr(pid vm.PID, vAddr uint64, level int) (uint64, int) {
	p.Lock()
	defer p.Unlock()

	k := ptKey{pid, level, ptPagePrefix(vAddr, level)}
	chiplet, ok := p.placement[k]
	if !ok {
		chiplet = 0
		p.placement[k] = chiplet
	}

	fk := ptFrameKey{k, chiplet}
	frame, ok := p.frames[fk]
	if !ok {
		frame = p.regionBase[chiplet] + p.nextFrame[chiplet]
		p.nextFrame[chiplet] += ptPageSize
		if p.nextFrame[chiplet] > p.regionSize {
			panic("page-table region exhausted")
		}
		p.frames[fk] = frame
	}

	return frame + pteIndex(vAddr, level)*pteSize, chiplet
}

// LeafPlacementHistogram returns how many leaf page-table pages each chiplet
// holds.
func (p *PTPlacer) LeafPlacementHistogram() []int {
	p.Lock()
	defer p.Unlock()

	h := make([]int, p.numChiplets)
	for k, c := range p.placement {
		if k.level == NumPTLevels-1 {
			h[c]++
		}
	}

	return h
}
