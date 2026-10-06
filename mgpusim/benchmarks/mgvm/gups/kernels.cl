// GUPS (giga-updates per second): every work-item performs a stream of
// read-modify-write updates to random locations of a large table.
#include "mgvm_cl.h"

__kernel void gups(__global uint *table, uint tableMask, uint updatesPerItem,
                   uint seed) {
  uint x = get_global_id(0) * 2654435761u + seed;
  for (uint n = 0; n < updatesPerItem; n++) {
    // xorshift32
    x ^= x << 13;
    x ^= x >> 17;
    x ^= x << 5;
    uint idx = x & tableMask;
    table[idx] ^= x;
  }
}
