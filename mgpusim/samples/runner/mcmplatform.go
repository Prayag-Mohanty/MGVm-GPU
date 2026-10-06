package runner

import (
	"fmt"

	"github.com/sarchlab/akita/v3/mem/mem"
	"github.com/sarchlab/akita/v3/mem/vm"
	"github.com/sarchlab/akita/v3/monitoring"
	"github.com/sarchlab/akita/v3/noc/networking/pcie"
	"github.com/sarchlab/akita/v3/sim"
	"github.com/sarchlab/mgpusim/v3/driver"
	"github.com/sarchlab/mgpusim/v3/mgvm"
)

// MCMParams describes an MCM GPU (Table I of the MGvm paper).
type MCMParams struct {
	VM          mgvm.Config
	Placement   string // "lasp" or "rr"
	NumChiplets int
	NumSA       int // shader arrays per chiplet
	CUPerSA     int

	L1TLBEntries int
	L2TLBEntries int
	L2TLBWays    int
	L2TLBLatency int
	L2TLBMSHR    int
	L2TLBWidth   int
	NumWalkers   int
	PWCEntries   int
	PWCLatency   int

	L2CacheSize     uint64
	NumMemBanks     int
	InterChipletLat int
	InterChipletBW  int // bytes per cycle per port

	Log2PageSize uint64
}

// DefaultMCMParams returns the simulation parameters of Table I.
func DefaultMCMParams(mode mgvm.Mode) MCMParams {
	return MCMParams{
		VM:              mgvm.DefaultConfig(mode, 4),
		Placement:       "lasp",
		NumChiplets:     4,
		NumSA:           8,
		CUPerSA:         4,
		L1TLBEntries:    32,
		L2TLBEntries:    512,
		L2TLBWays:       8,
		L2TLBLatency:    10,
		L2TLBMSHR:       64,
		L2TLBWidth:      2,
		NumWalkers:      16,
		PWCEntries:      32,
		PWCLatency:      10,
		L2CacheSize:     4 * mem.MB,
		NumMemBanks:     16,
		InterChipletLat: 32,
		InterChipletBW:  768,
		Log2PageSize:    12,
	}
}

// buildMCMPlatform builds a 4-chiplet MCM GPU whose chiplets are connected
// by a low-latency in-package network, with the requested virtual memory
// design.
func (r *Runner) buildMCMPlatform(p MCMParams) {
	p.VM.NumChiplets = p.NumChiplets
	p.VM.InterChipletLatency = p.InterChipletLat
	p.VM.Log2PageSize = p.Log2PageSize

	b := MakeR9NanoBuilder().
		WithNumGPU(p.NumChiplets).
		WithLog2PageSize(p.Log2PageSize)
	b.numSAPerGPU = p.NumSA
	b.numCUPerSA = p.CUPerSA

	if *magicMemoryCopy {
		b = b.WithMagicMemoryCopy()
	}

	r.monitor = monitoring.NewMonitor()
	if *customPortForAkitaRTM != 0 {
		r.monitor = r.monitor.WithPortNumber(*customPortForAkitaRTM)
	}
	b = b.WithMonitor(r.monitor)

	r.platform = b.buildMCM(p)
	r.monitor.StartServer()
}

func (b R9NanoPlatformBuilder) buildMCM(p MCMParams) *Platform {
	b.engine = b.createEngine()
	if b.monitor != nil {
		b.monitor.RegisterEngine(b.engine)
	}

	b.globalStorage = mem.NewStorage(uint64(1+b.numGPU) * 4 * mem.GB)

	mmuComponent, pageTable := b.createMMU(b.engine)
	gpuDriver := b.buildGPUDriver(pageTable)

	freq := 1 * sim.GHz
	rt := mgvm.NewRuntime(p.VM, b.engine, freq)

	// Page-table pages live in a reserved region of each chiplet's memory
	// (above the first 3GB, which hold data).
	regionBase := make([]uint64, p.NumChiplets)
	for c := range regionBase {
		regionBase[c] = uint64(c+1)*4*mem.GB + 3*mem.GB
	}
	rt.Placer = mgvm.NewPTPlacer(regionBase, 512*mem.MB)

	b.configureDriverForMCM(gpuDriver, rt, p)

	vmParams := &mcmVMParams{
		runtime: rt,
		l2TLBBuilder: mgvm.L2TLBBuilder{
			Engine:       b.engine,
			Freq:         freq,
			Log2PageSize: p.Log2PageSize,
			NumEntries:   p.L2TLBEntries,
			NumWays:      p.L2TLBWays,
			Latency:      p.L2TLBLatency,
			Width:        p.L2TLBWidth,
			NumMSHR:      p.L2TLBMSHR,
			NumChiplets:  p.NumChiplets,
		},
		walkerBuilder: mgvm.PageWalkerBuilder{
			Engine:     b.engine,
			Freq:       freq,
			NumWalkers: p.NumWalkers,
			PWCEntries: p.PWCEntries,
			PWCLatency: p.PWCLatency,
			PageTable:  pageTable,
			Placer:     rt.Placer,
		},
	}

	gpuBuilder := b.createGPUBuilder(b.engine, gpuDriver, mmuComponent).
		WithMCMVM(vmParams).
		WithL1TLBEntries(p.L1TLBEntries).
		WithL2CacheSize(p.L2CacheSize).
		WithNumMemoryBank(p.NumMemBanks)

	pcieConnector, rootComplexID :=
		b.createConnection(b.engine, gpuDriver, mmuComponent)
	mmuComponent.MigrationServiceProvider = gpuDriver.GetPortByName("MMU")

	network := mgvm.NewInterChipletNetwork("InterChipletNetwork",
		b.engine, freq, p.InterChipletLat, p.InterChipletBW)

	rdmaAddressTable := b.createRDMAAddrTable()
	pmcAddressTable := b.createPMCPageTable()

	switchID := pcieConnector.AddSwitch(rootComplexID)
	for i := 1; i <= p.NumChiplets; i++ {
		gpu := b.createMCMChiplet(i, gpuBuilder, gpuDriver,
			rdmaAddressTable, pmcAddressTable, pcieConnector, switchID, p)
		network.PlugIn(gpu.RDMAEngine.ToOutside, 64)
		network.PlugIn(gpu.RTU.GetPortByName("Remote"), 64)
	}

	remoteRTUs := make([]sim.Port, 0, p.NumChiplets)
	for _, gpu := range b.gpus {
		remoteRTUs = append(remoteRTUs, gpu.RTU.GetPortByName("Remote"))
	}
	for _, gpu := range b.gpus {
		gpu.RTU.RemoteRTUs = remoteRTUs
	}

	pcieConnector.EstablishRoute()

	return &Platform{
		Engine: b.engine,
		Driver: gpuDriver,
		GPUs:   b.gpus,
		MGvm:   rt,
	}
}

func (b *R9NanoPlatformBuilder) configureDriverForMCM(
	d *driver.Driver,
	rt *mgvm.Runtime,
	p MCMParams,
) {
	switch p.Placement {
	case "lasp":
		d.SetUnifiedGPUPlacement(driver.PlacementLASPBlock, rt.Cfg.LeafCoverage())
		d.SetCTAPolicy(driver.CTAPolicyContiguous)
	case "rr":
		d.SetUnifiedGPUPlacement(driver.PlacementRoundRobin, rt.Cfg.LeafCoverage())
		d.SetCTAPolicy(driver.CTAPolicyRoundRobin)
	default:
		panic(fmt.Sprintf("unknown placement %q (lasp|rr)", p.Placement))
	}

	d.SetPagePlacedHook(func(page vm.Page) {
		if page.DeviceID >= 1 {
			rt.OnDataPagePlaced(page, int(page.DeviceID)-1)
		}
	})
	d.SetKernelLaunchHook(rt.OnKernelLaunch)
}

// createMCMChiplet builds one chiplet. Unlike createGPU, the RDMA engine is
// connected to the in-package network instead of PCIe.
func (b *R9NanoPlatformBuilder) createMCMChiplet(
	index int,
	gpuBuilder R9NanoGPUBuilder,
	gpuDriver *driver.Driver,
	rdmaAddressTable *mem.BankedLowModuleFinder,
	pmcAddressTable *mem.BankedLowModuleFinder,
	pcieConnector *pcie.Connector,
	pcieSwitchID int,
	p MCMParams,
) *GPU {
	name := fmt.Sprintf("GPU[%d]", index)
	memAddrOffset := uint64(index) * 4 * mem.GB
	gpu := gpuBuilder.
		WithMemAddrOffset(memAddrOffset).
		Build(name, uint64(index))
	gpuDriver.RegisterGPU(
		gpu.Domain.GetPortByName("CommandProcessor"),
		driver.DeviceProperties{
			CUCount:  p.NumSA * p.CUPerSA,
			DRAMSize: 4 * mem.GB,
		},
	)
	gpu.CommandProcessor.Driver = gpuDriver.GetPortByName("GPU")

	b.configRDMAEngine(gpu, rdmaAddressTable)
	b.configPMC(gpu, gpuDriver, pmcAddressTable)

	pcieConnector.PlugInDevice(pcieSwitchID, []sim.Port{
		gpu.Domain.GetPortByName("CommandProcessor"),
		gpu.Domain.GetPortByName("PageMigrationController"),
	})

	b.gpus = append(b.gpus, gpu)

	return gpu
}
