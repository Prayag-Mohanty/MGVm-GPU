// Minimal OpenCL work-item helpers that do not require the ROCm device
// libraries. Kernels are compiled with clang -nogpulib for gfx803 (the GCN3
// ISA modeled by MGPUSim). Global offsets are always zero in MGPUSim.
#ifndef MGVM_CL_H
#define MGVM_CL_H

static inline uint mgvm_local_size(uint dim) {
  __constant ushort *dp = (__constant ushort *)__builtin_amdgcn_dispatch_ptr();
  return dp[2 + dim];
}

static inline uint mgvm_group_id(uint dim) {
  switch (dim) {
  case 0: return __builtin_amdgcn_workgroup_id_x();
  case 1: return __builtin_amdgcn_workgroup_id_y();
  default: return __builtin_amdgcn_workgroup_id_z();
  }
}

static inline uint mgvm_local_id(uint dim) {
  switch (dim) {
  case 0: return __builtin_amdgcn_workitem_id_x();
  case 1: return __builtin_amdgcn_workitem_id_y();
  default: return __builtin_amdgcn_workitem_id_z();
  }
}

#define get_local_size(d) mgvm_local_size(d)
#define get_group_id(d) mgvm_group_id(d)
#define get_local_id(d) mgvm_local_id(d)
static inline void mgvm_barrier(void) {
  __builtin_amdgcn_fence(__ATOMIC_RELEASE, "workgroup");
  __builtin_amdgcn_s_barrier();
  __builtin_amdgcn_fence(__ATOMIC_ACQUIRE, "workgroup");
}

#define barrier(flags) mgvm_barrier()
#define get_global_id(d) (mgvm_group_id(d) * mgvm_local_size(d) + mgvm_local_id(d))

#endif
