package mgvm

import (
	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/sim"
)

// XlatReq is an address translation request travelling between RTUs and L2
// TLB slices. It wraps the request issued by an L1 TLB.
type XlatReq struct {
	sim.MsgMeta

	PID    vm.PID
	VAddr  uint64
	Origin int // chiplet whose L1 TLB missed
	L1Req  *vm.TranslationReq
	Hops   int // number of times the request has been rerouted

	IssueTime sim.VTimeInSec
}

// Meta returns the message meta data.
func (r *XlatReq) Meta() *sim.MsgMeta {
	return &r.MsgMeta
}

// XlatRsp is the response of an XlatReq.
type XlatRsp struct {
	sim.MsgMeta

	Req       *XlatReq
	Page      vm.Page
	RemoteHit bool
}

// Meta returns the message meta data.
func (r *XlatRsp) Meta() *sim.MsgMeta {
	return &r.MsgMeta
}

const xlatMsgBytes = 16

func newXlatReq(
	now sim.VTimeInSec,
	src, dst sim.Port,
	pid vm.PID,
	vAddr uint64,
	origin int,
	l1Req *vm.TranslationReq,
) *XlatReq {
	r := &XlatReq{
		PID:       pid,
		VAddr:     vAddr,
		Origin:    origin,
		L1Req:     l1Req,
		IssueTime: now,
	}
	r.ID = sim.GetIDGenerator().Generate()
	r.Src = src
	r.Dst = dst
	r.SendTime = now
	r.TrafficBytes = xlatMsgBytes

	return r
}

// forward creates a copy of the request that is sent from src to dst.
func (r *XlatReq) forward(now sim.VTimeInSec, src, dst sim.Port) *XlatReq {
	c := *r
	c.ID = sim.GetIDGenerator().Generate()
	c.Src = src
	c.Dst = dst
	c.SendTime = now

	return &c
}

func newXlatRsp(
	now sim.VTimeInSec,
	src, dst sim.Port,
	req *XlatReq,
	page vm.Page,
) *XlatRsp {
	r := &XlatRsp{Req: req, Page: page}
	r.ID = sim.GetIDGenerator().Generate()
	r.Src = src
	r.Dst = dst
	r.SendTime = now
	r.TrafficBytes = xlatMsgBytes

	return r
}

func (r *XlatRsp) forward(now sim.VTimeInSec, src, dst sim.Port) *XlatRsp {
	c := *r
	c.ID = sim.GetIDGenerator().Generate()
	c.Src = src
	c.Dst = dst
	c.SendTime = now

	return &c
}
