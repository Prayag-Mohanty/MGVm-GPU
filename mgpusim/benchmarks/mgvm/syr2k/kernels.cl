// PolyBench SYR2K: C = alpha*A*B^T + alpha*B*A^T + beta*C, A and B are N x M.
#include "mgvm_cl.h"

__kernel void syr2k(__global const float *A, __global const float *B,
                    __global float *C, float alpha, float beta, int N, int M) {
  int j = get_global_id(0);
  int i = get_global_id(1);
  if (i < N && j < N) {
    float acc = C[i * N + j] * beta;
    for (int k = 0; k < M; k++) {
      acc += alpha * A[i * M + k] * B[j * M + k] +
             alpha * B[i * M + k] * A[j * M + k];
    }
    C[i * N + j] = acc;
  }
}
