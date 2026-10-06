package mgvm

import (
	"log"
	"reflect"

	"github.com/sarchlab/akita/v3/mem/mem"
	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
)

// pwc is a fully associative, LRU page-walk cache. An entry for (level l,
// page prefix) caches the pointer to the level-l page-table page, so a hit on
// level l lets the walk start at level l.
type pwc struct {
	capacity int
	clock    uint64
	entries  map[ptKey]uint64
}

func newPWC(capacity int) *pwc {
	return &pwc{capacity: capacity, entries: make(map[ptKey]uint64)}
}

// startLevel returns the level at which a walk for vAddr starts, based on
// the longest-prefix match in the cache (0 means a full 4-level walk).
func (c *pwc) startLevel(pid vm.PID, vAddr uint64) int {
	for l := NumPTLevels - 1; l >= 1; l-- {
		k := ptKey{pid, l, ptPagePrefix(vAddr, l)}
		if _, ok := c.entries[k]; ok {
			c.clock++
			c.entries[k] = c.clock
			return l
		}
	}

	return 0
}

func (c *pwc) insert(pid vm.PID, vAddr uint64, level int) {
	k := ptKey{pid, level, ptPagePrefix(vAddr, level)}
	c.clock++
	if _, ok := c.entries[k]; !ok && len(c.entries) >= c.capacity {
		var victim ptKey
		oldest := ^uint64(0)
		for key, t := range c.entries {
			if t < oldest {
				oldest = t
				victim = key
			}
		}
		delete(c.entries, victim)
	}
	c.entries[k] = c.clock
}

type walk struct {
	req       *vm.TranslationReq
	level     int
	pwcReady  sim.VTimeInSec
	memReq    *mem.ReadReq
	startTime sim.VTimeInSec
}

// WalkerStats are the statistics collected by the page-table walkers.
type WalkerStats struct {
	Walks            uint64
	TotalLatency     float64 // seconds, from arrival to completion
	PTELocalAccesses uint64
	PTERemoteAccess  uint64
	PWCHits          uint64 // walks that skipped at least one level
}

// PageWalker models the page-table walkers (PTWs) and the page-walk cache of
// one chiplet. Each of the NumWalkers walkers performs one walk at a time,
// issuing a memory read per page-table level. The reads go through the
// chiplet's memory hierarchy (L2 cache, or the RDMA engine for PTEs that
// reside in another chiplet's memory), so remote PTEs cost inter-chiplet
// latency.
type PageWalker struct {
	*sim.TickingComponent

	Chiplet   int
	PageTable vm.PageTable
	Placer    *PTPlacer
	MemFinder mem.LowModuleFinder

	topPort sim.Port
	memPort sim.Port

	topSender sim.BufferedSender
	memSender sim.BufferedSender

	freq       sim.Freq
	numWalkers int
	pwcLatency int
	pwc        *pwc

	queue   []*walk
	active  []*walk
	memReqs map[string]*walk

	Stats WalkerStats
}

// PageWalkerBuilder builds page walkers.
type PageWalkerBuilder struct {
	Engine     sim.Engine
	Freq       sim.Freq
	NumWalkers int
	PWCEntries int
	PWCLatency int
	PageTable  vm.PageTable
	Placer     *PTPlacer
}

// Build creates the page walker of a chiplet.
func (b PageWalkerBuilder) Build(name string, chiplet int) *PageWalker {
	w := &PageWalker{
		Chiplet:    chiplet,
		PageTable:  b.PageTable,
		Placer:     b.Placer,
		freq:       b.Freq,
		numWalkers: b.NumWalkers,
		pwcLatency: b.PWCLatency,
		pwc:        newPWC(b.PWCEntries),
		memReqs:    make(map[string]*walk),
	}
	w.TickingComponent = sim.NewTickingComponent(name, b.Engine, b.Freq, w)

	w.topPort = sim.NewLimitNumMsgPort(w, 64, name+".TopPort")
	w.memPort = sim.NewLimitNumMsgPort(w, 64, name+".MemPort")
	w.AddPort("Top", w.topPort)
	w.AddPort("Mem", w.memPort)

	w.topSender = sim.NewBufferedSender(w.topPort,
		sim.NewBuffer(name+".TopSendBuf", 1<<20))
	w.memSender = sim.NewBufferedSender(w.memPort,
		sim.NewBuffer(name+".MemSendBuf", 1<<20))

	return w
}

// Tick updates the walker state.
func (w *PageWalker) Tick(now sim.VTimeInSec) bool {
	madeProgress := false

	madeProgress = w.topSender.Tick(now) || madeProgress
	madeProgress = w.memSender.Tick(now) || madeProgress
	madeProgress = w.parseMem(now) || madeProgress
	madeProgress = w.issue(now) || madeProgress
	madeProgress = w.startWalks(now) || madeProgress
	madeProgress = w.acceptFromTop(now) || madeProgress

	waitingOnPWC := false
	for _, a := range w.active {
		if a.memReq == nil {
			waitingOnPWC = true
		}
	}

	return madeProgress || waitingOnPWC
}

func (w *PageWalker) acceptFromTop(now sim.VTimeInSec) bool {
	madeProgress := false
	for {
		msg := w.topPort.Retrieve(now)
		if msg == nil {
			return madeProgress
		}

		req, ok := msg.(*vm.TranslationReq)
		if !ok {
			log.Panicf("page walker cannot handle %s", reflect.TypeOf(msg))
		}

		w.queue = append(w.queue, &walk{req: req, startTime: now})
		madeProgress = true
	}
}

func (w *PageWalker) startWalks(now sim.VTimeInSec) bool {
	madeProgress := false
	for len(w.queue) > 0 && len(w.active) < w.numWalkers {
		wk := w.queue[0]
		w.queue = w.queue[1:]

		wk.level = w.pwc.startLevel(wk.req.PID, wk.req.VAddr)
		if wk.level > 0 {
			w.Stats.PWCHits++
		}
		wk.pwcReady = w.freq.NCyclesLater(w.pwcLatency, now)
		w.active = append(w.active, wk)
		madeProgress = true
	}

	return madeProgress
}

// issue sends the memory read for every active walk that is ready to access
// its next level.
func (w *PageWalker) issue(now sim.VTimeInSec) bool {
	madeProgress := false
	for _, wk := range w.active {
		if wk.memReq != nil || wk.pwcReady > now+1e-15 {
			continue
		}

		if !w.memSender.CanSend(1) {
			break
		}

		addr, chiplet := w.Placer.PTEAddr(wk.req.PID, wk.req.VAddr, wk.level)
		if chiplet == w.Chiplet {
			w.Stats.PTELocalAccesses++
		} else {
			w.Stats.PTERemoteAccess++
		}

		read := mem.ReadReqBuilder{}.
			WithSendTime(now).
			WithSrc(w.memPort).
			WithDst(w.MemFinder.Find(addr)).
			WithAddress(addr).
			WithByteSize(pteSize).
			Build()
		w.memSender.Send(read)
		wk.memReq = read
		w.memReqs[read.ID] = wk
		madeProgress = true
	}

	return madeProgress
}

func (w *PageWalker) parseMem(now sim.VTimeInSec) bool {
	madeProgress := false
	for {
		msg := w.memPort.Peek()
		if msg == nil {
			return madeProgress
		}

		rsp, ok := msg.(*mem.DataReadyRsp)
		if !ok {
			log.Panicf("page walker cannot handle %s", reflect.TypeOf(msg))
		}

		wk, found := w.memReqs[rsp.RespondTo]
		if !found {
			w.memPort.Retrieve(now)
			madeProgress = true
			continue
		}

		if wk.level == NumPTLevels-1 && !w.topSender.CanSend(1) {
			return madeProgress
		}

		w.memPort.Retrieve(now)
		delete(w.memReqs, rsp.RespondTo)
		wk.memReq = nil
		madeProgress = true

		if wk.level < NumPTLevels-1 {
			// The PTE just read points to the next-level page; remember it.
			wk.level++
			w.pwc.insert(wk.req.PID, wk.req.VAddr, wk.level)
			wk.pwcReady = now
			continue
		}

		w.finish(now, wk)
	}
}

func (w *PageWalker) finish(now sim.VTimeInSec, wk *walk) {
	page, found := w.PageTable.Find(wk.req.PID, wk.req.VAddr)
	if !found {
		log.Panicf("page 0x%x of PID %d not found", wk.req.VAddr, wk.req.PID)
	}

	rsp := vm.TranslationRspBuilder{}.
		WithSendTime(now).
		WithSrc(w.topPort).
		WithDst(wk.req.Src).
		WithRspTo(wk.req.ID).
		WithPage(page).
		Build()
	w.topSender.Send(rsp)

	w.Stats.Walks++
	w.Stats.TotalLatency += float64(now - wk.startTime)

	for i, a := range w.active {
		if a == wk {
			w.active = append(w.active[:i], w.active[i+1:]...)
			break
		}
	}
}
