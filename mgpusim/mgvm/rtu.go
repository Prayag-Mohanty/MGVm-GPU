package mgvm

import (
	"log"
	"reflect"

	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
)

// maxReroutes bounds how often a request may be rerouted between components
// that temporarily disagree on the HSL. After that it is served wherever it
// is (correctness is not affected since TLBs are read-only caches).
const maxReroutes = 4

// RTUStats are the statistics collected by an RTU.
type RTUStats struct {
	LocalRequests uint64 // L1 TLB misses served by the local L2 TLB slice
	Outgoing      uint64 // L1 TLB misses sent to a remote L2 TLB slice
	Incoming      uint64 // requests received from remote chiplets
	Reroutes      uint64 // requests forwarded again due to an HSL mismatch
}

// rtuEpoch holds the per-epoch counters used for imbalance detection.
type rtuEpoch struct {
	incoming, outgoing, serviced uint64
}

// RTU is the Remote Translation Unit of a chiplet. All L1 TLB misses of the
// chiplet go through it. Using its copy of the HSL, it forwards a miss to the
// local L2 TLB slice or to the RTU of the home chiplet. It also forwards
// responses back to the requesting L1 TLB (or remote RTU) and monitors the
// incoming/outgoing remote translation traffic for MGvm's dHSL-balance.
type RTU struct {
	*sim.TickingComponent

	Chiplet int
	View    *HSLView

	// L2TLB is the top port of the local L2 TLB slice.
	L2TLB sim.Port
	// RemoteRTUs[c] is the remote port of the RTU of chiplet c.
	RemoteRTUs []sim.Port

	// Monitor receives the epoch counters. May be nil.
	Monitor *Runtime

	topPort    sim.Port
	l2Port     sim.Port
	remotePort sim.Port

	topSender    sim.BufferedSender
	l2Sender     sim.BufferedSender
	remoteSender sim.BufferedSender

	epochSize     uint64
	epoch         rtuEpoch
	LastEpoch     rtuEpoch
	possibleCount int

	Stats RTUStats
}

// NewRTU creates an RTU.
func NewRTU(
	name string,
	engine sim.Engine,
	freq sim.Freq,
	chiplet int,
	epochSize uint64,
) *RTU {
	r := &RTU{Chiplet: chiplet, epochSize: epochSize}
	r.TickingComponent = sim.NewTickingComponent(name, engine, freq, r)

	r.topPort = sim.NewLimitNumMsgPort(r, 64, name+".TopPort")
	r.l2Port = sim.NewLimitNumMsgPort(r, 64, name+".L2Port")
	r.remotePort = sim.NewLimitNumMsgPort(r, 64, name+".RemotePort")
	r.AddPort("Top", r.topPort)
	r.AddPort("L2", r.l2Port)
	r.AddPort("Remote", r.remotePort)

	r.topSender = sim.NewBufferedSender(r.topPort,
		sim.NewBuffer(name+".TopSendBuf", 1<<20))
	r.l2Sender = sim.NewBufferedSender(r.l2Port,
		sim.NewBuffer(name+".L2SendBuf", 1<<20))
	r.remoteSender = sim.NewBufferedSender(r.remotePort,
		sim.NewBuffer(name+".RemoteSendBuf", 1<<20))

	return r
}

// Tick updates the RTU state.
func (r *RTU) Tick(now sim.VTimeInSec) bool {
	madeProgress := false

	madeProgress = r.topSender.Tick(now) || madeProgress
	madeProgress = r.l2Sender.Tick(now) || madeProgress
	madeProgress = r.remoteSender.Tick(now) || madeProgress

	for i := 0; i < 4; i++ {
		madeProgress = r.parseRemote(now) || madeProgress
		madeProgress = r.parseL2(now) || madeProgress
		madeProgress = r.parseTop(now) || madeProgress
	}

	return madeProgress
}

// parseTop handles misses coming from the L1 TLBs of this chiplet.
func (r *RTU) parseTop(now sim.VTimeInSec) bool {
	msg := r.topPort.Retrieve(now)
	if msg == nil {
		return false
	}

	req, ok := msg.(*vm.TranslationReq)
	if !ok {
		log.Panicf("RTU cannot handle %s", reflect.TypeOf(msg))
	}

	home := r.View.Get().Home(req.VAddr, r.Chiplet)
	if home == r.Chiplet {
		x := newXlatReq(now, r.l2Port, r.L2TLB, req.PID, req.VAddr, r.Chiplet, req)
		r.l2Sender.Send(x)
		r.Stats.LocalRequests++

		return true
	}

	x := newXlatReq(now, r.remotePort, r.RemoteRTUs[home],
		req.PID, req.VAddr, r.Chiplet, req)
	r.remoteSender.Send(x)
	r.Stats.Outgoing++
	r.epoch.outgoing++
	r.countServiced(now)

	return true
}

// parseRemote handles requests and responses from other chiplets.
func (r *RTU) parseRemote(now sim.VTimeInSec) bool {
	msg := r.remotePort.Retrieve(now)
	if msg == nil {
		return false
	}

	switch m := msg.(type) {
	case *XlatReq:
		r.Stats.Incoming++
		r.epoch.incoming++
		r.countServiced(now)

		// The RTU assumes its own HSL is the latest one and reroutes the
		// request if it does not belong to this chiplet.
		home := r.View.Get().Home(m.VAddr, m.Origin)
		if home != r.Chiplet && m.Hops < maxReroutes {
			fwd := m.forward(now, r.remotePort, r.RemoteRTUs[home])
			fwd.Hops++
			r.remoteSender.Send(fwd)
			r.Stats.Reroutes++

			return true
		}

		r.l2Sender.Send(m.forward(now, r.l2Port, r.L2TLB))
	case *XlatRsp:
		r.respondToL1(now, m)
	default:
		log.Panicf("RTU cannot handle %s", reflect.TypeOf(msg))
	}

	return true
}

// parseL2 handles responses and redirected requests from the local L2 TLB.
func (r *RTU) parseL2(now sim.VTimeInSec) bool {
	msg := r.l2Port.Retrieve(now)
	if msg == nil {
		return false
	}

	switch m := msg.(type) {
	case *XlatRsp:
		if m.Req.Origin == r.Chiplet {
			r.respondToL1(now, m)
		} else {
			r.remoteSender.Send(
				m.forward(now, r.remotePort, r.RemoteRTUs[m.Req.Origin]))
		}
	case *XlatReq:
		// Redirected by the L2 TLB slice because of an HSL mismatch.
		home := r.View.Get().Home(m.VAddr, m.Origin)
		r.Stats.Reroutes++
		if home == r.Chiplet {
			r.l2Sender.Send(m.forward(now, r.l2Port, r.L2TLB))
		} else {
			r.remoteSender.Send(m.forward(now, r.remotePort, r.RemoteRTUs[home]))
		}
	default:
		log.Panicf("RTU cannot handle %s", reflect.TypeOf(msg))
	}

	return true
}

func (r *RTU) respondToL1(now sim.VTimeInSec, rsp *XlatRsp) {
	l1Req := rsp.Req.L1Req
	out := vm.TranslationRspBuilder{}.
		WithSendTime(now).
		WithSrc(r.topPort).
		WithDst(l1Req.Src).
		WithRspTo(l1Req.ID).
		WithPage(rsp.Page).
		Build()
	r.topSender.Send(out)
}

// countServiced advances the epoch. At the end of an epoch the RTU checks
// whether it may be the target of an imbalance (Possible := Incoming >
// 2*Outgoing) and, if so for two consecutive epochs, alerts the runtime.
func (r *RTU) countServiced(now sim.VTimeInSec) {
	r.epoch.serviced++
	if r.epoch.serviced < r.epochSize {
		return
	}

	r.LastEpoch = r.epoch
	r.epoch = rtuEpoch{}

	if r.Monitor == nil {
		return
	}

	if r.LastEpoch.incoming > 2*r.LastEpoch.outgoing {
		r.possibleCount++
	} else {
		r.possibleCount = 0
	}

	if r.possibleCount >= 2 {
		r.possibleCount = 0
		r.Monitor.Trigger(now, r.Chiplet)
	}

	r.Monitor.EpochEnded(now, r.Chiplet)
}
