// SHOC reduction: every work-group sums a contiguous chunk of the input in
// local memory and writes one partial sum.
#include "mgvm_cl.h"

#define WG 256

__kernel void reduce(__global const float *in, __global float *partial,
                     uint elemsPerGroup) {
  __local float sdata[WG];
  uint tid = get_local_id(0);
  uint base = get_group_id(0) * elemsPerGroup;

  float sum = 0.0f;
  for (uint i = tid; i < elemsPerGroup; i += WG) {
    sum += in[base + i];
  }
  sdata[tid] = sum;
  barrier(CLK_LOCAL_MEM_FENCE);

  for (uint s = WG / 2; s > 0; s >>= 1) {
    if (tid < s) {
      sdata[tid] += sdata[tid + s];
    }
    barrier(CLK_LOCAL_MEM_FENCE);
  }

  if (tid == 0) {
    partial[get_group_id(0)] = sdata[0];
  }
}
