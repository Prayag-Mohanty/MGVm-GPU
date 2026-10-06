// PolyBench 2DCONV: 3x3 convolution of an NI x NJ matrix.
#include "mgvm_cl.h"

__kernel void conv2d(__global const float *A, __global float *B, int NI,
                     int NJ) {
  int j = get_global_id(0);
  int i = get_global_id(1);
  const float c11 = +0.2f, c21 = +0.5f, c31 = -0.8f;
  const float c12 = -0.3f, c22 = +0.6f, c32 = -0.9f;
  const float c13 = +0.4f, c23 = +0.7f, c33 = +0.10f;
  if (i > 0 && i < NI - 1 && j > 0 && j < NJ - 1) {
    B[i * NJ + j] =
        c11 * A[(i - 1) * NJ + (j - 1)] + c12 * A[(i + 0) * NJ + (j - 1)] +
        c13 * A[(i + 1) * NJ + (j - 1)] + c21 * A[(i - 1) * NJ + (j + 0)] +
        c22 * A[(i + 0) * NJ + (j + 0)] + c23 * A[(i + 1) * NJ + (j + 0)] +
        c31 * A[(i - 1) * NJ + (j + 1)] + c32 * A[(i + 0) * NJ + (j + 1)] +
        c33 * A[(i + 1) * NJ + (j + 1)];
  }
}
