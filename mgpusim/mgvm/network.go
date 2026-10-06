package mgvm

import (
	"github.com/sarchlab/akita/v3/sim"
)

type netMsg struct {
	msg     sim.Msg
	arrival sim.VTimeInSec
}

type netEnd struct {
	port     sim.Port
	buf      []sim.Msg
	bufSize  int
	busy     bool
	inFlight []netMsg
}

// InterChipletNetwork models the in-package interconnect of an MCM GPU. Every
// message experiences a fixed latency (default 32 cycles at 1GHz = 32ns) and
// each port can inject at most BytesPerCycle bytes per cycle (768 GB/s at
// 1GHz by default).
type InterChipletNetwork struct {
	*sim.TickingComponent

	Latency       int
	BytesPerCycle int

	freq  sim.Freq
	ports []sim.Port
	ends  map[sim.Port]*netEnd
	next  int
}

// NewInterChipletNetwork creates a new network.
func NewInterChipletNetwork(
	name string,
	engine sim.Engine,
	freq sim.Freq,
	latency, bytesPerCycle int,
) *InterChipletNetwork {
	n := &InterChipletNetwork{
		Latency:       latency,
		BytesPerCycle: bytesPerCycle,
		freq:          freq,
		ends:          make(map[sim.Port]*netEnd),
	}
	n.TickingComponent = sim.NewSecondaryTickingComponent(name, engine, freq, n)

	return n
}

// PlugIn connects a port to the network.
func (n *InterChipletNetwork) PlugIn(port sim.Port, sourceSideBufSize int) {
	n.Lock()
	defer n.Unlock()

	n.ports = append(n.ports, port)
	n.ends[port] = &netEnd{port: port, bufSize: sourceSideBufSize}
	port.SetConnection(n)
}

// Unplug is not supported.
func (n *InterChipletNetwork) Unplug(_ sim.Port) {
	panic("not implemented")
}

// NotifyAvailable is called when a destination port can receive again.
func (n *InterChipletNetwork) NotifyAvailable(now sim.VTimeInSec, _ sim.Port) {
	n.TickNow(now)
}

// CanSend checks if the source port can inject a message.
func (n *InterChipletNetwork) CanSend(src sim.Port) bool {
	n.Lock()
	defer n.Unlock()

	end := n.ends[src]
	if len(end.buf) >= end.bufSize {
		end.busy = true
		return false
	}

	return true
}

// Send injects a message into the network.
func (n *InterChipletNetwork) Send(msg sim.Msg) *sim.SendError {
	n.Lock()
	defer n.Unlock()

	src := msg.Meta().Src
	end, ok := n.ends[src]
	if !ok {
		panic("source port not connected to " + n.Name())
	}
	if _, ok := n.ends[msg.Meta().Dst]; !ok {
		panic("destination port " + msg.Meta().Dst.Name() +
			" not connected to " + n.Name())
	}

	if len(end.buf) >= end.bufSize {
		end.busy = true
		return sim.NewSendError()
	}

	end.buf = append(end.buf, msg)
	n.TickNow(msg.Meta().SendTime)

	return nil
}

// Tick injects buffered messages and delivers arrived ones.
func (n *InterChipletNetwork) Tick(now sim.VTimeInSec) bool {
	n.Lock()
	defer n.Unlock()

	madeProgress := false
	pending := false

	for i := 0; i < len(n.ports); i++ {
		end := n.ends[n.ports[(i+n.next)%len(n.ports)]]
		madeProgress = n.deliver(now, end) || madeProgress
		madeProgress = n.inject(now, end) || madeProgress

		if len(end.inFlight) > 0 || len(end.buf) > 0 {
			pending = true
		}
	}
	n.next++

	// Keep ticking while messages are in flight so that they are delivered
	// when their latency elapses.
	return madeProgress || pending
}

func (n *InterChipletNetwork) inject(now sim.VTimeInSec, end *netEnd) bool {
	budget := n.BytesPerCycle
	madeProgress := false
	arrival := n.freq.NCyclesLater(n.Latency, now)

	for len(end.buf) > 0 {
		msg := end.buf[0]
		size := msg.Meta().TrafficBytes
		if size <= 0 {
			size = 64
		}
		if size > budget && budget < n.BytesPerCycle {
			break
		}
		budget -= size

		end.inFlight = append(end.inFlight, netMsg{msg: msg, arrival: arrival})
		end.buf = end.buf[1:]
		madeProgress = true

		if end.busy {
			end.busy = false
			end.port.NotifyAvailable(now)
		}
	}

	return madeProgress
}

func (n *InterChipletNetwork) deliver(now sim.VTimeInSec, end *netEnd) bool {
	madeProgress := false
	for len(end.inFlight) > 0 {
		head := end.inFlight[0]
		if head.arrival > now+1e-15 {
			break
		}

		head.msg.Meta().RecvTime = now
		if err := head.msg.Meta().Dst.Recv(head.msg); err != nil {
			break
		}

		end.inFlight = end.inFlight[1:]
		madeProgress = true
	}

	return madeProgress
}
