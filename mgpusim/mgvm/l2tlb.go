package mgvm

import (
	"log"
	"reflect"

	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/mem/vm/tlb"
	"github.com/sarchlab/akita/v3/sim"
)

type tlbEntry struct {
	valid    bool
	pid      vm.PID
	vpn      uint64
	page     vm.Page
	lastUsed uint64
}

type tlbPipeItem struct {
	req   *XlatReq
	ready sim.VTimeInSec
}

type mshrKey struct {
	pid vm.PID
	vpn uint64
}

type tlbMSHREntry struct {
	reqs []*XlatReq
}

// L2TLBStats are the statistics collected by an L2 TLB slice.
type L2TLBStats struct {
	Accesses   uint64
	Hits       uint64
	LocalHits  uint64 // hits for requests from CUs of the same chiplet
	RemoteHits uint64 // hits for requests from other chiplets
	Misses     uint64 // misses that started a page walk
	MSHRHits   uint64 // misses merged into an outstanding walk
	Redirects  uint64 // requests bounced back because of an HSL mismatch
}

// L2TLB is the L2 TLB slice of a chiplet. Under a private-TLB configuration
// it only serves its own chiplet; otherwise it is one slice of a logically
// shared L2 TLB and serves the VA ranges that the HSL maps to it.
type L2TLB struct {
	*sim.TickingComponent

	Chiplet int
	View    *HSLView

	// RTU is the port of the local RTU that responses and redirected
	// requests are sent to.
	RTU sim.Port
	// Walker is the top port of the local page-table walker.
	Walker sim.Port

	// TagHSL returns the dHSL-coarse function used to tag TLB accesses for
	// detecting when the imbalance disappears. May be nil.
	TagHSL func() HSL

	topPort     sim.Port
	bottomPort  sim.Port
	controlPort sim.Port

	topSender    sim.BufferedSender
	bottomSender sim.BufferedSender

	freq         sim.Freq
	log2PageSize uint64
	numSets      int
	numWays      int
	latency      int
	width        int
	numMSHR      int

	sets     [][]tlbEntry
	useClock uint64
	pipeline []tlbPipeItem
	mshr     map[mshrKey]*tlbMSHREntry
	walkReqs map[string]mshrKey

	Stats L2TLBStats

	// TagCounts counts, for each chiplet c, the number of accesses whose VA
	// dHSL-coarse maps to c. It is reset by the runtime every epoch.
	TagCounts []uint64
}

// L2TLBBuilder builds L2 TLB slices.
type L2TLBBuilder struct {
	Engine       sim.Engine
	Freq         sim.Freq
	Log2PageSize uint64
	NumEntries   int
	NumWays      int
	Latency      int
	Width        int
	NumMSHR      int
	NumChiplets  int
}

// Build creates an L2 TLB slice.
func (b L2TLBBuilder) Build(name string, chiplet int) *L2TLB {
	t := &L2TLB{
		Chiplet:      chiplet,
		freq:         b.Freq,
		log2PageSize: b.Log2PageSize,
		numWays:      b.NumWays,
		numSets:      b.NumEntries / b.NumWays,
		latency:      b.Latency,
		width:        b.Width,
		numMSHR:      b.NumMSHR,
		mshr:         make(map[mshrKey]*tlbMSHREntry),
		walkReqs:     make(map[string]mshrKey),
		TagCounts:    make([]uint64, b.NumChiplets),
	}
	t.TickingComponent = sim.NewTickingComponent(name, b.Engine, b.Freq, t)

	t.topPort = sim.NewLimitNumMsgPort(t, 4*b.Width, name+".TopPort")
	t.bottomPort = sim.NewLimitNumMsgPort(t, 4*b.Width, name+".BottomPort")
	t.controlPort = sim.NewLimitNumMsgPort(t, 1, name+".ControlPort")
	t.AddPort("Top", t.topPort)
	t.AddPort("Bottom", t.bottomPort)
	t.AddPort("Control", t.controlPort)

	t.topSender = sim.NewBufferedSender(t.topPort,
		sim.NewBuffer(name+".TopSendBuf", 1<<20))
	t.bottomSender = sim.NewBufferedSender(t.bottomPort,
		sim.NewBuffer(name+".BottomSendBuf", 1<<20))

	t.sets = make([][]tlbEntry, t.numSets)
	for i := range t.sets {
		t.sets[i] = make([]tlbEntry, t.numWays)
	}

	return t
}

// Tick updates the state of the TLB.
func (t *L2TLB) Tick(now sim.VTimeInSec) bool {
	madeProgress := false

	madeProgress = t.topSender.Tick(now) || madeProgress
	madeProgress = t.bottomSender.Tick(now) || madeProgress
	madeProgress = t.handleControl(now) || madeProgress

	for i := 0; i < t.width; i++ {
		madeProgress = t.parseBottom(now) || madeProgress
	}

	for i := 0; i < t.width; i++ {
		madeProgress = t.finishLookup(now) || madeProgress
	}

	for i := 0; i < t.width; i++ {
		madeProgress = t.acceptFromTop(now) || madeProgress
	}

	// Requests in the lookup pipeline need the TLB to keep ticking.
	return madeProgress || len(t.pipeline) > 0
}

func (t *L2TLB) acceptFromTop(now sim.VTimeInSec) bool {
	if len(t.pipeline) >= t.latency*t.width {
		return false
	}

	msg := t.topPort.Peek()
	if msg == nil {
		return false
	}

	req, ok := msg.(*XlatReq)
	if !ok {
		log.Panicf("L2 TLB cannot handle %s", reflect.TypeOf(msg))
	}

	t.topPort.Retrieve(now)
	t.pipeline = append(t.pipeline, tlbPipeItem{
		req:   req,
		ready: t.freq.NCyclesLater(t.latency, now),
	})

	return true
}

func (t *L2TLB) finishLookup(now sim.VTimeInSec) bool {
	if len(t.pipeline) == 0 {
		return false
	}

	item := t.pipeline[0]
	if item.ready > now+1e-15 {
		return false
	}

	req := item.req

	// An L2 TLB slice only serves the VA ranges the HSL maps to it. Requests
	// that arrive here because a component used a stale HSL are sent back to
	// the RTU, which forwards them to the right slice.
	home := t.View.Get().Home(req.VAddr, req.Origin)
	if home != t.Chiplet && req.Hops < maxReroutes {
		if !t.topSender.CanSend(1) {
			return false
		}
		fwd := req.forward(now, t.topPort, t.RTU)
		fwd.Hops++
		t.topSender.Send(fwd)
		t.Stats.Redirects++
		t.pipeline = t.pipeline[1:]

		return true
	}

	vpn := req.VAddr >> t.log2PageSize
	key := mshrKey{req.PID, vpn}

	if e, found := t.lookup(req.PID, vpn); found {
		if !t.topSender.CanSend(1) {
			return false
		}

		t.respond(now, req, e.page)
		t.countAccess(req)
		t.Stats.Hits++
		if req.Origin == t.Chiplet {
			t.Stats.LocalHits++
		} else {
			t.Stats.RemoteHits++
		}
		t.pipeline = t.pipeline[1:]

		return true
	}

	if m, ok := t.mshr[key]; ok {
		m.reqs = append(m.reqs, req)
		t.countAccess(req)
		t.Stats.MSHRHits++
		t.pipeline = t.pipeline[1:]

		return true
	}

	if len(t.mshr) >= t.numMSHR || !t.bottomSender.CanSend(1) {
		return false
	}

	walkReq := vm.TranslationReqBuilder{}.
		WithSendTime(now).
		WithSrc(t.bottomPort).
		WithDst(t.Walker).
		WithPID(req.PID).
		WithVAddr(req.VAddr).
		WithDeviceID(uint64(t.Chiplet)).
		Build()
	t.bottomSender.Send(walkReq)

	t.mshr[key] = &tlbMSHREntry{reqs: []*XlatReq{req}}
	t.walkReqs[walkReq.ID] = key
	t.countAccess(req)
	t.Stats.Misses++
	t.pipeline = t.pipeline[1:]

	return true
}

func (t *L2TLB) countAccess(req *XlatReq) {
	t.Stats.Accesses++
	if t.TagHSL != nil {
		t.TagCounts[t.TagHSL().Home(req.VAddr, req.Origin)]++
	}
}

func (t *L2TLB) respond(now sim.VTimeInSec, req *XlatReq, page vm.Page) {
	rsp := newXlatRsp(now, t.topPort, t.RTU, req, page)
	rsp.RemoteHit = req.Origin != t.Chiplet
	t.topSender.Send(rsp)
}

func (t *L2TLB) parseBottom(now sim.VTimeInSec) bool {
	msg := t.bottomPort.Peek()
	if msg == nil {
		return false
	}

	rsp := msg.(*vm.TranslationRsp)
	key, ok := t.walkReqs[rsp.RespondTo]
	if !ok {
		t.bottomPort.Retrieve(now)
		return true
	}

	m := t.mshr[key]
	if !t.topSender.CanSend(len(m.reqs)) {
		return false
	}

	t.insert(key.pid, key.vpn, rsp.Page)
	for _, r := range m.reqs {
		t.respond(now, r, rsp.Page)
	}

	delete(t.mshr, key)
	delete(t.walkReqs, rsp.RespondTo)
	t.bottomPort.Retrieve(now)

	return true
}

func (t *L2TLB) setID(vpn uint64) int {
	return int(vpn % uint64(t.numSets))
}

func (t *L2TLB) lookup(pid vm.PID, vpn uint64) (*tlbEntry, bool) {
	set := t.sets[t.setID(vpn)]
	for i := range set {
		e := &set[i]
		if e.valid && e.pid == pid && e.vpn == vpn {
			t.useClock++
			e.lastUsed = t.useClock
			return e, true
		}
	}

	return nil, false
}

func (t *L2TLB) insert(pid vm.PID, vpn uint64, page vm.Page) {
	set := t.sets[t.setID(vpn)]
	victim := 0
	for i := range set {
		if !set[i].valid {
			victim = i
			break
		}
		if set[i].lastUsed < set[victim].lastUsed {
			victim = i
		}
	}

	t.useClock++
	set[victim] = tlbEntry{
		valid:    true,
		pid:      pid,
		vpn:      vpn,
		page:     page,
		lastUsed: t.useClock,
	}
}

func (t *L2TLB) handleControl(now sim.VTimeInSec) bool {
	msg := t.controlPort.Peek()
	if msg == nil {
		return false
	}

	switch req := msg.(type) {
	case *tlb.FlushReq:
		rsp := tlb.FlushRspBuilder{}.
			WithSrc(t.controlPort).WithDst(req.Src).WithSendTime(now).Build()
		if err := t.controlPort.Send(rsp); err != nil {
			return false
		}
		for _, vAddr := range req.VAddr {
			vpn := vAddr >> t.log2PageSize
			if e, ok := t.lookup(req.PID, vpn); ok {
				e.valid = false
			}
		}
	case *tlb.RestartReq:
		rsp := tlb.RestartRspBuilder{}.
			WithSrc(t.controlPort).WithDst(req.Src).WithSendTime(now).Build()
		if err := t.controlPort.Send(rsp); err != nil {
			return false
		}
	default:
		log.Panicf("cannot handle %s", reflect.TypeOf(msg))
	}

	t.controlPort.Retrieve(now)

	return true
}
