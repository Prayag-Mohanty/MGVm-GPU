package runner

import (
	"flag"
	"log"
	"os"

	"github.com/sarchlab/mgpusim/v3/mgvm"
	"github.com/tebeka/atexit"
)

var vmFlag = flag.String("vm", "",
	"Simulate an MCM GPU with the given virtual memory design: "+
		"private, shared, mgvm-nobalance or mgvm. Implies -timing and "+
		"-unified-gpus=1,2,3,4.")
var placementFlag = flag.String("placement", "lasp",
	"CTA scheduling and data placement on the MCM GPU: lasp or rr (naive).")
var vmStatsFlag = flag.String("vm-stats", "vm_stats.csv",
	"File to write the virtual memory statistics of the MCM GPU to.")
var l2TLBEntriesFlag = flag.Int("l2tlb-entries", 512,
	"Number of entries of each chiplet's L2 TLB slice.")
var numWalkersFlag = flag.Int("num-walkers", 16,
	"Number of page table walkers per chiplet.")
var interChipletLatFlag = flag.Int("interchiplet-latency", 32,
	"Inter-chiplet latency in cycles (1 GHz).")

func (r *Runner) mcmMode() bool {
	return *vmFlag != ""
}

func (r *Runner) initMCM() {
	*timingFlag = true
	r.Timing = true
	if *unifiedGPUFlag == "" {
		*unifiedGPUFlag = "1,2,3,4"
	}
	if *gpuFlag != "" {
		log.Panic("-vm cannot be used with -gpus")
	}
}

func (r *Runner) buildMCMPlatformFromFlags() {
	p := DefaultMCMParams(mgvm.ParseMode(*vmFlag))
	p.NumChiplets = len(r.GPUIDs)
	p.Placement = *placementFlag
	p.L2TLBEntries = *l2TLBEntriesFlag
	p.NumWalkers = *numWalkersFlag
	p.InterChipletLat = *interChipletLatFlag

	r.buildMCMPlatform(p)

	atexit.Register(func() {
		f, err := os.Create(*vmStatsFlag)
		if err != nil {
			log.Panic(err)
		}
		defer f.Close()
		r.platform.MGvm.Report(f)
	})
}
