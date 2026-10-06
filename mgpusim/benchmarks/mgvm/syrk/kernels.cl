// PolyBench SYRK: C = alpha * A * A^T + beta * C, A is N x M, C is N x N.
#include "mgvm_cl.h"

__kernel void syrk(__global const float *A, __global float *C, float alpha,
                   float beta, int N, int M) {
  int j = get_global_id(0);
  int i = get_global_id(1);
  if (i < N && j < N) {
    float acc = C[i * N + j] * beta;
    for (int k = 0; k < M; k++) {
      acc += alpha * A[i * M + k] * A[j * M + k];
    }
    C[i * N + j] = acc;
  }
}
