package kernels

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"log"

	"github.com/sarchlab/mgpusim/v3/insts"
)

// kernelDescriptor is the 64-byte AMDHSA kernel descriptor used by code
// object v3 and later (the "<kernel>.kd" symbol).
type kernelDescriptor struct {
	GroupSegmentFixedSize   uint32
	PrivateSegmentFixedSize uint32
	KernargSize             uint32
	Reserved0               [4]byte
	KernelCodeEntryOffset   int64
	Reserved1               [20]byte
	ComputePgmRsrc3         uint32
	ComputePgmRsrc1         uint32
	ComputePgmRsrc2         uint32
	KernelCodeProperties    uint16
	Reserved2               [6]byte
}

// LoadProgramFromMemoryCOV4 loads a kernel from an HSACO produced by a modern
// LLVM (code object v3/v4, e.g. clang-18 targeting gfx803). MGPUSim natively
// understands only the code object v2 layout (an amd_kernel_code_t header
// followed by the code), so this function synthesizes the v2 header from the
// v3+ kernel descriptor and prepends it to the kernel's machine code.
func LoadProgramFromMemoryCOV4(data []byte, kernelName string) *insts.HsaCo {
	executable, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}

	symbols, err := executable.Symbols()
	if err != nil {
		log.Fatal(err)
	}

	var codeSym, kdSym *elf.Symbol
	for i := range symbols {
		switch symbols[i].Name {
		case kernelName:
			codeSym = &symbols[i]
		case kernelName + ".kd":
			kdSym = &symbols[i]
		}
	}

	if codeSym == nil || kdSym == nil {
		return nil
	}

	kd := readKernelDescriptor(executable, kdSym)
	code := readSymbolBytes(executable, codeSym)

	header := new(insts.HsaCoHeader)
	header.CodeVersionMajor = 1
	header.CodeVersionMinor = 0
	header.MachineKind = 1
	header.MachineVersionMajor = 8
	header.KernelCodeEntryByteOffset = 256
	header.ComputePgmRsrc1 = kd.ComputePgmRsrc1
	header.ComputePgmRsrc2 = kd.ComputePgmRsrc2
	// Bits 0-6 of kernel_code_properties have the same meaning in the v3
	// descriptor and in amd_kernel_code_t.
	header.Flags = uint32(kd.KernelCodeProperties & 0x7f)
	header.WIPrivateSegmentByteSize = kd.PrivateSegmentFixedSize
	header.WGGroupSegmentByteSize = kd.GroupSegmentFixedSize
	header.KernargSegmentByteSize = uint64(kd.KernargSize)
	header.WFSgprCount = uint16((extract(kd.ComputePgmRsrc1, 6, 9) + 1) * 8)
	header.WIVgprCount = uint16((extract(kd.ComputePgmRsrc1, 0, 5) + 1) * 4)
	header.KernargSegmentAlignment = 4
	header.GroupSegmentAlignment = 4
	header.PrivateSegmentAlignment = 4
	header.WavefrontSize = 6

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, header); err != nil {
		log.Fatal(err)
	}
	if buf.Len() != 256 {
		log.Panicf("unexpected amd_kernel_code_t size %d", buf.Len())
	}
	buf.Write(code)

	hsaco := insts.NewHsaCoFromData(buf.Bytes())

	// In code object v2 the kernel symbol covers the 256-byte header as well
	// as the code. The timing model relies on this to bound instruction
	// fetching.
	sym := *codeSym
	sym.Size = uint64(len(hsaco.Data))
	hsaco.Symbol = &sym

	return hsaco
}

func extract(v uint32, lo, hi int) uint32 {
	mask := uint32((1 << (hi - lo + 1)) - 1)
	return (v >> lo) & mask
}

func readSymbolBytes(f *elf.File, sym *elf.Symbol) []byte {
	sec := f.Sections[sym.Section]
	secData, err := sec.Data()
	if err != nil {
		log.Fatal(err)
	}

	offset := sym.Value - sec.Addr
	size := sym.Size
	if size == 0 {
		size = uint64(len(secData)) - offset
	}

	return secData[offset : offset+size]
}

func readKernelDescriptor(f *elf.File, sym *elf.Symbol) kernelDescriptor {
	raw := readSymbolBytes(f, sym)
	kd := kernelDescriptor{}
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &kd); err != nil {
		log.Fatal(err)
	}

	return kd
}
