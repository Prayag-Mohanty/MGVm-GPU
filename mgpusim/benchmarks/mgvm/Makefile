# Compiles the MGvm benchmark kernels (OpenCL C) to gfx803 HSACO code objects
# (code object v4) using a stock LLVM/clang toolchain. MGPUSim loads them with
# kernels.LoadProgramFromMemoryCOV4.
CLANG ?= clang
CFLAGS = -x cl -cl-std=CL1.2 -target amdgcn-amd-amdhsa -mcpu=gfx803 \
	-mcode-object-version=4 -nogpulib -O2 -Iinclude

KERNELS = $(wildcard */kernels.cl)
HSACO = $(KERNELS:.cl=.hsaco)

all: $(HSACO)

%.hsaco: %.cl include/mgvm_cl.h
	$(CLANG) $(CFLAGS) -o $@ $<

clean:
	rm -f $(HSACO)
