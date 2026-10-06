package mgvm

import (
	"fmt"
	"io"
	"log"

	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
	"github.com/sarchlab/mgpusim/v3/driver"
)

// Mode is the virtual memory configuration of the MCM GPU.
type Mode string

// Supported configurations (Section VI of the paper).
const (
	ModePrivate       Mode = "private"
	ModeShared        Mode = "shared"
	ModeMGvmNoBalance Mode = "mgvm-nobalance"
	ModeMGvm          Mode = "mgvm"
)

// ParseMode converts a string to a Mode.
func ParseMode(s string) Mode {
	switch Mode(s) {
	case ModePrivate, ModeShared, ModeMGvmNoBalance, ModeMGvm:
		return Mode(s)
	}

	log.Panicf("unknown VM mode %q (private|shared|mgvm-nobalance|mgvm)", s)

	return ""
}

// Config holds the parameters of the MGvm runtime.
type Config struct {
	Mode        Mode
	NumChiplets int

	Log2PageSize uint64

	// FineGranularity is the interleaving granularity of the shared L2 TLB
	// and of dHSL-balance (4KB by default).
	FineGranularity uint64

	// EpochSize is the number of remote requests per RTU epoch.
	EpochSize uint64

	ImbalanceThreshold  float64 // default 0.8
	HitRateThreshold    float64 // default 0.9
	SwitchBackThreshold float64 // default 0.5

	// InterChipletLatency in cycles, used to delay HSL switch messages.
	InterChipletLatency int
}

// DefaultConfig returns the default parameters of the paper.
func DefaultConfig(mode Mode, numChiplets int) Config {
	return Config{
		Mode:                mode,
		NumChiplets:         numChiplets,
		Log2PageSize:        12,
		FineGranularity:     4096,
		EpochSize:           5000,
		ImbalanceThreshold:  0.8,
		HitRateThreshold:    0.9,
		SwitchBackThreshold: 0.5,
		InterChipletLatency: 32,
	}
}

// LeafCoverage returns the VA range covered by one leaf page-table page.
func (c Config) LeafCoverage() uint64 {
	return (uint64(1) << c.Log2PageSize) * (ptPageSize / pteSize)
}

// SwitchRecord logs one HSL switch.
type SwitchRecord struct {
	Time sim.VTimeInSec
	HSL  HSL
}

type hslSwitchEvent struct {
	*sim.EventBase
	chiplet int
	hsl     HSL
}

// Runtime is the MGvm logic in the GPU driver and the command processor. At
// every kernel launch it selects the HSL and places the leaf page-table
// pages (Listing 1); during the execution it decides when to switch to
// dHSL-balance (Listing 2) and back.
type Runtime struct {
	Cfg    Config
	Engine sim.Engine
	Freq   sim.Freq

	RTUs    []*RTU
	L2TLBs  []*L2TLB
	Walkers []*PageWalker
	Placer  *PTPlacer

	rtuViews []*HSLView
	l2Views  []*HSLView

	coarse        HSL
	balanced      bool
	prevImbalance bool
	switchBackCnt int
	lastHits      uint64
	lastAccesses  uint64

	Kernels  []KernelRecord
	Switches []SwitchRecord
	Triggers uint64
}

// KernelRecord stores the HSL selected for a kernel launch.
type KernelRecord struct {
	Time        sim.VTimeInSec
	HSL         HSL
	LargestSize uint64
}

// NewRuntime creates a runtime.
func NewRuntime(cfg Config, engine sim.Engine, freq sim.Freq) *Runtime {
	return &Runtime{Cfg: cfg, Engine: engine, Freq: freq}
}

// Register connects the runtime with the per-chiplet hardware.
func (rt *Runtime) Register(rtu *RTU, l2 *L2TLB, w *PageWalker) {
	rtu.View = &HSLView{}
	l2.View = &HSLView{}
	rt.rtuViews = append(rt.rtuViews, rtu.View)
	rt.l2Views = append(rt.l2Views, l2.View)

	rt.RTUs = append(rt.RTUs, rtu)
	rt.L2TLBs = append(rt.L2TLBs, l2)
	rt.Walkers = append(rt.Walkers, w)

	if rt.Cfg.Mode == ModeMGvm {
		rtu.Monitor = rt
		l2.TagHSL = func() HSL { return rt.coarse }
	}

	rt.setAllViews(rt.initialHSL())
}

func (rt *Runtime) initialHSL() HSL {
	switch rt.Cfg.Mode {
	case ModePrivate:
		return HSL{Kind: HSLPrivate, NumChiplets: rt.Cfg.NumChiplets}
	case ModeShared:
		return rt.fineHSL()
	default:
		return HSL{
			Kind:        HSLCoarse,
			Granularity: rt.Cfg.LeafCoverage(),
			NumChiplets: rt.Cfg.NumChiplets,
		}
	}
}

func (rt *Runtime) fineHSL() HSL {
	return HSL{
		Kind:        HSLInterleave,
		Granularity: rt.Cfg.FineGranularity,
		NumChiplets: rt.Cfg.NumChiplets,
	}
}

func (rt *Runtime) setAllViews(h HSL) {
	for _, v := range rt.rtuViews {
		v.Set(h)
	}
	for _, v := range rt.l2Views {
		v.Set(h)
	}
}

// OnDataPagePlaced must be called by the driver whenever a data page is
// mapped (the baseline first-placement policy for page-table pages).
func (rt *Runtime) OnDataPagePlaced(page vm.Page, chiplet int) {
	rt.Placer.OnDataPagePlaced(page.PID, page.VAddr, chiplet)
}

// OnKernelLaunch implements Listing 1 of the paper.
func (rt *Runtime) OnKernelLaunch(
	now sim.VTimeInSec,
	_ vm.PID,
	allocs []driver.Allocation,
) {
	if rt.Cfg.Mode != ModeMGvm && rt.Cfg.Mode != ModeMGvmNoBalance {
		return
	}

	var largest driver.Allocation
	for _, a := range allocs {
		if a.Size > largest.Size {
			largest = a
		}
	}

	pageSize := uint64(1) << rt.Cfg.Log2PageSize
	n := uint64(rt.Cfg.NumChiplets)
	unit := rt.Cfg.LeafCoverage()

	// LASP partitions the largest allocation into one contiguous block per
	// chiplet. dHSL-coarse uses the same block size, rounded up to a
	// multiple of the VA range covered by a leaf page-table page.
	numPages := (largest.Size + pageSize - 1) / pageSize
	laspBlk := (numPages + n - 1) / n * pageSize
	gran := (laspBlk + unit - 1) / unit * unit
	if gran == 0 {
		gran = unit
	}

	// The driver aligns allocations to the leaf coverage (2MB). Instead of
	// moving the largest allocation to a power-of-two aligned VA, the HSL
	// carries a base register so that block i of the largest allocation
	// maps to chiplet i.
	rt.coarse = HSL{
		Kind:        HSLCoarse,
		Granularity: gran,
		Base:        largest.VAddr,
		NumChiplets: rt.Cfg.NumChiplets,
	}

	rt.Placer.PlaceLeafPagesByHSL(rt.coarse)
	rt.setAllViews(rt.coarse)
	rt.balanced = false
	rt.prevImbalance = false
	rt.switchBackCnt = 0
	rt.Kernels = append(rt.Kernels, KernelRecord{now, rt.coarse, largest.Size})
}

// Trigger is called by an RTU that observed a possible imbalance for two
// consecutive epochs. It implements the decision flow of Listing 2. The
// counters are read immediately; the round trip to collect them from all
// RTUs and L2 TLBs is modeled as a delay of the resulting switch.
func (rt *Runtime) Trigger(now sim.VTimeInSec, _ int) {
	rt.Triggers++
	if rt.balanced {
		return
	}

	total := uint64(0)
	for _, r := range rt.RTUs {
		total += r.LastEpoch.incoming
	}

	imbalance := false
	for _, r := range rt.RTUs {
		if total > 0 &&
			float64(r.LastEpoch.incoming)/float64(total) > rt.Cfg.ImbalanceThreshold {
			imbalance = true
		}
	}

	hits, accesses := uint64(0), uint64(0)
	for _, t := range rt.L2TLBs {
		hits += t.Stats.Hits
		accesses += t.Stats.Accesses
	}
	dHits, dAccesses := hits-rt.lastHits, accesses-rt.lastAccesses
	rt.lastHits, rt.lastAccesses = hits, accesses

	hitRate := 0.0
	if dAccesses > 0 {
		hitRate = float64(dHits) / float64(dAccesses)
	}

	if !imbalance || hitRate <= rt.Cfg.HitRateThreshold {
		rt.prevImbalance = false
		return
	}

	if !rt.prevImbalance {
		rt.prevImbalance = true
		return
	}

	rt.balanced = true
	rt.prevImbalance = false
	rt.switchBackCnt = 0
	rt.broadcastSwitch(now, rt.fineHSL())
}

// EpochEnded is called by the RTUs at the end of each epoch. While running
// with dHSL-balance it checks whether the imbalance has disappeared, using
// the per-chiplet tag counters of the L2 TLBs.
func (rt *Runtime) EpochEnded(now sim.VTimeInSec, chiplet int) {
	if !rt.balanced || chiplet != 0 {
		return
	}

	counts := make([]uint64, rt.Cfg.NumChiplets)
	total := uint64(0)
	for _, t := range rt.L2TLBs {
		for c, v := range t.TagCounts {
			counts[c] += v
			total += v
			t.TagCounts[c] = 0
		}
	}

	if total == 0 {
		return
	}

	imbalance := false
	for _, v := range counts {
		if float64(v)/float64(total) > rt.Cfg.SwitchBackThreshold {
			imbalance = true
		}
	}

	if imbalance {
		rt.switchBackCnt = 0
		return
	}

	rt.switchBackCnt++
	if rt.switchBackCnt >= 2 {
		rt.balanced = false
		rt.switchBackCnt = 0
		rt.broadcastSwitch(now, rt.coarse)
	}
}

// broadcastSwitch sends the HSL switch message to all RTUs and L2 TLBs. The
// command processor sits on chiplet 0; the message reaches the other
// chiplets after the inter-chiplet latency, so components switch
// asynchronously. Collecting the counters (a round trip) precedes the
// decision.
func (rt *Runtime) broadcastSwitch(now sim.VTimeInSec, h HSL) {
	lat := rt.Cfg.InterChipletLatency
	decision := rt.Freq.NCyclesLater(2*lat, now)
	rt.Switches = append(rt.Switches, SwitchRecord{decision, h})

	for c := 0; c < rt.Cfg.NumChiplets; c++ {
		delay := lat
		if c == 0 {
			delay = 1
		}
		t := rt.Freq.NCyclesLater(delay, decision)
		evt := &hslSwitchEvent{
			EventBase: sim.NewEventBase(t, rt),
			chiplet:   c,
			hsl:       h,
		}
		rt.Engine.Schedule(evt)
	}
}

// Handle applies a scheduled HSL switch to the components of one chiplet.
func (rt *Runtime) Handle(e sim.Event) error {
	evt := e.(*hslSwitchEvent)
	rt.rtuViews[evt.chiplet].Set(evt.hsl)
	rt.l2Views[evt.chiplet].Set(evt.hsl)

	return nil
}

// Report writes the virtual-memory statistics as "name, value" CSV lines.
func (rt *Runtime) Report(w io.Writer) {
	p := func(name string, v interface{}) {
		fmt.Fprintf(w, "%s, %v\n", name, v)
	}

	var l2 L2TLBStats
	var rtu RTUStats
	var walks, pteLocal, pteRemote, pwcHits uint64
	var walkLatency float64

	for i, t := range rt.L2TLBs {
		s := t.Stats
		p(fmt.Sprintf("chiplet%d.l2tlb.accesses", i), s.Accesses)
		p(fmt.Sprintf("chiplet%d.l2tlb.hits", i), s.Hits)
		p(fmt.Sprintf("chiplet%d.l2tlb.misses", i), s.Misses)
		l2.Accesses += s.Accesses
		l2.Hits += s.Hits
		l2.LocalHits += s.LocalHits
		l2.RemoteHits += s.RemoteHits
		l2.Misses += s.Misses
		l2.MSHRHits += s.MSHRHits
		l2.Redirects += s.Redirects
	}

	for i, r := range rt.RTUs {
		s := r.Stats
		p(fmt.Sprintf("chiplet%d.rtu.incoming", i), s.Incoming)
		p(fmt.Sprintf("chiplet%d.rtu.outgoing", i), s.Outgoing)
		rtu.LocalRequests += s.LocalRequests
		rtu.Outgoing += s.Outgoing
		rtu.Incoming += s.Incoming
		rtu.Reroutes += s.Reroutes
	}

	for _, wk := range rt.Walkers {
		walks += wk.Stats.Walks
		walkLatency += wk.Stats.TotalLatency
		pteLocal += wk.Stats.PTELocalAccesses
		pteRemote += wk.Stats.PTERemoteAccess
		pwcHits += wk.Stats.PWCHits
	}

	p("vm.mode", rt.Cfg.Mode)
	p("l2tlb.accesses", l2.Accesses)
	p("l2tlb.hits", l2.Hits)
	p("l2tlb.local_hits", l2.LocalHits)
	p("l2tlb.remote_hits", l2.RemoteHits)
	p("l2tlb.misses", l2.Misses)
	p("l2tlb.mshr_hits", l2.MSHRHits)
	p("l2tlb.redirects", l2.Redirects)
	p("rtu.local_requests", rtu.LocalRequests)
	p("rtu.remote_requests", rtu.Outgoing)
	p("rtu.reroutes", rtu.Reroutes)
	p("ptw.walks", walks)
	p("ptw.pwc_hits", pwcHits)
	p("ptw.pte_local_accesses", pteLocal)
	p("ptw.pte_remote_accesses", pteRemote)
	if walks > 0 {
		p("ptw.avg_latency_cycles", walkLatency/float64(walks)*float64(rt.Freq))
	} else {
		p("ptw.avg_latency_cycles", 0)
	}
	p("mgvm.kernels", len(rt.Kernels))
	for i, k := range rt.Kernels {
		p(fmt.Sprintf("mgvm.kernel%d.hsl", i), k.HSL)
	}
	p("mgvm.triggers", rt.Triggers)
	p("mgvm.switches", len(rt.Switches))
	for i, s := range rt.Switches {
		p(fmt.Sprintf("mgvm.switch%d", i), fmt.Sprintf("%.9f %s", s.Time, s.HSL))
	}
	if rt.Placer != nil {
		for c, v := range rt.Placer.LeafPlacementHistogram() {
			p(fmt.Sprintf("chiplet%d.leaf_pt_pages", c), v)
		}
	}
}
