// Maximal independent set (Luby's algorithm, as in Pannotia's MIS) on a
// graph in CSR format. status: -1 undecided, 1 in the set, 0 excluded.

#include "mgvm_cl.h"

// Marks undecided nodes whose value is the smallest among their undecided
// neighbors.
__kernel void mis_select(__global const int *rowPtr, __global const int *cols,
                         __global const uint *value, __global const int *status,
                         __global int *selected, __global int *undecided,
                         int numNodes) {
  int v = get_global_id(0);
  if (v >= numNodes) {
    return;
  }

  selected[v] = 0;
  if (status[v] != -1) {
    return;
  }

  undecided[0] = 1;
  uint myVal = value[v];
  int isMin = 1;
  for (int e = rowPtr[v]; e < rowPtr[v + 1]; e++) {
    int u = cols[e];
    if (status[u] == -1) {
      uint uVal = value[u];
      if (uVal < myVal || (uVal == myVal && u < v)) {
        isMin = 0;
        break;
      }
    }
  }
  selected[v] = isMin;
}

// Adds the selected nodes to the set and excludes their neighbors.
__kernel void mis_update(__global const int *rowPtr, __global const int *cols,
                         __global const int *selected, __global int *status,
                         int numNodes) {
  int v = get_global_id(0);
  if (v >= numNodes || selected[v] == 0) {
    return;
  }

  status[v] = 1;
  for (int e = rowPtr[v]; e < rowPtr[v + 1]; e++) {
    status[cols[e]] = 0;
  }
}
