// PolyBench JACOBI-1D: one time step, B[i] = (A[i-1] + A[i] + A[i+1]) / 3.
#include "mgvm_cl.h"

__kernel void jacobi1d(__global const float *A, __global float *B, int N) {
  int i = get_global_id(0);
  if (i > 0 && i < N - 1) {
    B[i] = 0.33333f * (A[i - 1] + A[i] + A[i + 1]);
  }
}
