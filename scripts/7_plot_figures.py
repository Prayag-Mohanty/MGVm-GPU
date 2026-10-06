#!/usr/bin/python3
"""Plots Figures 7-10 of the paper from normalized.csv.

Usage: ./7_plot_figures.py normalized.csv [output folder (default: figures)]

  fig7_throughput.png      throughput normalized to private TLB (+ geomean)
  fig8_l2tlb_hits.png      local vs remote L2 TLB hits (shared TLB, MGvm)
  fig9_pw_accesses.png     local vs remote page-walk memory accesses
  fig10_pw_latency.png     page-walk latency normalized to private TLB
"""

import csv
import math
import os
import sys

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt  # noqa: E402

SHORT = {
    "convolution2d": "C2D", "fastwalshtransform": "FW", "gups": "GUPS",
    "jacobi1d": "J1D", "jacobi2d": "J2D", "kmeans": "KM",
    "matrixtranspose": "MT", "mis": "MIS", "pagerank": "PR",
    "simpleconvolution": "SC", "shoc-reduction": "RED", "spmv": "SPMV",
    "stencil2d": "S2D", "syrk": "SYRK", "syr2k": "SYR2",
}
CONFIGS = ["Private", "Shared", "MGVM-NoBalance", "MGVM"]
LABELS = {"Private": "Private TLB", "Shared": "Shared TLB",
          "MGVM-NoBalance": "MGvm-nobalance", "MGVM": "MGvm"}
COLORS = {"Private": "#f2e6c9", "Shared": "#f0a830",
          "MGVM-NoBalance": "#7a9cc6", "MGVM": "#2d5d9f"}
LOCAL, REMOTE = "#f0c040", "#3a6fb0"


def read(path):
    with open(path) as f:
        rows = list(csv.reader(f))
    header = rows[0]
    data = {}
    for r in rows[1:]:
        if not r or not r[0]:
            continue
        vals = {}
        for h, v in zip(header, r):
            try:
                vals[h] = float(v)
            except ValueError:
                pass
        data[r[0]] = vals
    return data


def geomean(xs):
    xs = [x for x in xs if x > 0 and math.isfinite(x)]
    return math.exp(sum(map(math.log, xs)) / len(xs)) if xs else float("nan")


def grouped(ax, names, series, configs, ylabel, gmean=False):
    n = len(configs)
    width = 0.8 / n
    xs = list(range(len(names)))
    if gmean:
        xs.append(len(names) + 0.5)
        names = names + ["Gmean"]
        for c in configs:
            series[c] = series[c] + [geomean(series[c])]
    for i, c in enumerate(configs):
        ax.bar([x + (i - (n - 1) / 2) * width for x in xs], series[c], width,
               label=LABELS[c], color=COLORS[c], edgecolor="black",
               linewidth=0.4)
    ax.set_xticks(xs)
    ax.set_xticklabels(names, rotation=90)
    ax.set_ylabel(ylabel)
    ax.legend(ncol=n, fontsize=8, loc="upper center",
              bbox_to_anchor=(0.5, 1.15), frameon=False)
    ax.grid(axis="y", linewidth=0.3)


def stacked(ax, names, configs, local_key, remote_key, data, ylabel):
    xs, labels, loc, rem = [], [], [], []
    x = 0
    for b in names:
        for c in configs:
            lv = data[b].get(local_key + c, 0.0)
            rv = data[b].get(remote_key + c, 0.0)
            xs.append(x)
            labels.append(f"{SHORT.get(b, b)}-{c[0] if c != 'MGVM' else 'M'}")
            loc.append(lv)
            rem.append(rv)
            x += 1
        x += 0.6
    ax.bar(xs, loc, color=LOCAL, edgecolor="black", linewidth=0.3,
           label="Local")
    ax.bar(xs, rem, bottom=loc, color=REMOTE, edgecolor="black",
           linewidth=0.3, label="Remote")
    ax.set_xticks(xs)
    ax.set_xticklabels(labels, rotation=90, fontsize=6)
    ax.set_ylabel(ylabel)
    ax.set_ylim(0, 1.05)
    ax.legend(ncol=2, fontsize=8, loc="upper center",
              bbox_to_anchor=(0.5, 1.12), frameon=False)


def main():
    data = read(sys.argv[1])
    out = sys.argv[2] if len(sys.argv) > 2 else "figures"
    os.makedirs(out, exist_ok=True)
    benches = [b for b in SHORT if b in data]
    names = [SHORT[b] for b in benches]

    fig, ax = plt.subplots(figsize=(10, 3.2))
    series = {c: [data[b].get("IPC " + c, float("nan")) for b in benches]
              for c in CONFIGS}
    grouped(ax, names, series, CONFIGS,
            "Throughput\n(normalized to Private TLB)", gmean=True)
    fig.tight_layout()
    fig.savefig(os.path.join(out, "fig7_throughput.png"), dpi=150)

    fig, ax = plt.subplots(figsize=(10, 3.2))
    stacked(ax, benches, ["Shared", "MGVM"], "L2 TLB Local Hits ",
            "L2 TLB Remote Hits ", data, "Fraction of L2 TLB hits")
    fig.tight_layout()
    fig.savefig(os.path.join(out, "fig8_l2tlb_hits.png"), dpi=150)

    fig, ax = plt.subplots(figsize=(12, 3.2))
    stacked(ax, benches, ["Private", "Shared", "MGVM"],
            "PW Acccess Local Hits ", "PW Acccess Remote Hits ", data,
            "Fraction of PW accesses")
    fig.tight_layout()
    fig.savefig(os.path.join(out, "fig9_pw_accesses.png"), dpi=150)

    fig, ax = plt.subplots(figsize=(10, 3.2))
    configs = ["Private", "Shared", "MGVM"]
    series = {c: [data[b].get("PW Latency " + c, float("nan")) for b in benches]
              for c in configs}
    grouped(ax, names, series, configs,
            "PW latency\n(normalized to Private TLB)", gmean=True)
    fig.tight_layout()
    fig.savefig(os.path.join(out, "fig10_pw_latency.png"), dpi=150)

    print("wrote figures to", out)


if __name__ == "__main__":
    main()
