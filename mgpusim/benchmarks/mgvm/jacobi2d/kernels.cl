// PolyBench JACOBI-2D: one time step of the 5-point stencil.
#include "mgvm_cl.h"

__kernel void jacobi2d(__global const float *A, __global float *B, int N) {
  int j = get_global_id(0);
  int i = get_global_id(1);
  if (i > 0 && i < N - 1 && j > 0 && j < N - 1) {
    B[i * N + j] = 0.2f * (A[i * N + j] + A[i * N + (j - 1)] +
                           A[i * N + (j + 1)] + A[(i + 1) * N + j] +
                           A[(i - 1) * N + j]);
  }
}
