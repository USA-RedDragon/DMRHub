#!/usr/bin/env python3
"""Summarize the results of hack/stress.sh and draw its graphs.

Usage: stress_plot.py RESULTS_DIR

Writes RESULTS_DIR/summary.csv and summary.md, and PNG graphs next to the
area results: the latency, losses and DB statements per burst of the load
ramp, the latency of each area, and the gauges sampled during each area.
Needs matplotlib for the graphs; the summary is written without it.
"""
import csv
import glob
import json
import os
import sys

BLUE, ORANGE, AQUA, YELLOW = "#2a78d6", "#eb6834", "#1baf7a", "#eda100"
INK, MUTED, GRID, SURFACE = "#0b0b0b", "#52514e", "#e4e3df", "#fcfcfb"


def load(results):
    out = []
    for path in sorted(glob.glob(os.path.join(results, "*", "*.json"))):
        with open(path) as f:
            try:
                d = json.load(f)
            except json.JSONDecodeError:
                continue
        d["_dir"] = os.path.dirname(path)
        out.append(d)
    return out


def summary(results, runs):
    rows = []
    for d in runs:
        n, lat = d.get("numbers", {}), d.get("latency_ms", {}) or {}
        main = lat.get("all") or lat.get("loaded_all") or lat.get("with_websockets") or {}
        rows.append({
            "backend": d.get("backend"), "area": d.get("area"), "passed": d.get("passed"),
            "failures": len(d.get("failures") or []), "elapsed_s": round(d.get("elapsed_s", 0), 1),
            "bursts": n.get("bursts_sent", ""), "missing": n.get("deliveries_missing", ""),
            "duplicates": n.get("deliveries_duplicate", ""), "wrong": n.get("deliveries_wrong_target", ""),
            "p50_ms": main.get("p50", ""), "p99_ms": main.get("p99", ""), "max_ms": main.get("max", ""),
            "db_queries_per_burst": round(n["db_queries_per_burst"], 1) if "db_queries_per_burst" in n else "",
            "loadavg": n.get("loadavg_start", ""),
        })
    if not rows:
        return rows
    with open(os.path.join(results, "summary.csv"), "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)
    with open(os.path.join(results, "summary.md"), "w") as f:
        cols = list(rows[0].keys())
        f.write("| " + " | ".join(cols) + " |\n|" + "---|" * len(cols) + "\n")
        for r in rows:
            f.write("| " + " | ".join(str(r[c]) for c in cols) + " |\n")
        f.write("\n")
        for d in runs:
            for msg in d.get("failures") or []:
                f.write(f"- {d.get('backend')} {d.get('area')}: {msg[:600]}\n")
    return rows


def style(ax, title, ylabel, xlabel=None):
    ax.set_title(title, loc="left", fontsize=11, color=INK)
    ax.set_ylabel(ylabel, color=MUTED, fontsize=9)
    if xlabel:
        ax.set_xlabel(xlabel, color=MUTED, fontsize=9)
    ax.grid(True, color=GRID, linewidth=0.8)
    ax.set_axisbelow(True)
    for s in ("top", "right"):
        ax.spines[s].set_visible(False)
    for s in ("left", "bottom"):
        ax.spines[s].set_color(GRID)
    ax.tick_params(colors=MUTED, labelsize=8)
    ax.set_facecolor(SURFACE)


def read_csv(path):
    with open(path) as f:
        return [{k: float(v) for k, v in r.items()} for r in csv.DictReader(f)]


def plot_ramp(plt, d):
    path = os.path.join(d["_dir"], "ramp.steps.csv")
    if not os.path.exists(path):
        return
    steps = read_csv(path)
    if not steps:
        return
    x = [s["repeaters"] for s in steps]
    fig, axes = plt.subplots(1, 3, figsize=(15, 4.2), facecolor=SURFACE)
    ax = axes[0]
    for key, label, color in (("p50_ms", "p50", BLUE), ("p99_ms", "p99", ORANGE), ("data_p99_ms", "data p99", AQUA)):
        ax.plot(x, [s[key] for s in steps], color=color, linewidth=2, marker="o", markersize=5, label=label)
    ax.set_yscale("symlog", linthresh=10)
    style(ax, "Per-burst forwarding latency", "ms", "busy repeaters")
    ax.legend(frameon=False, fontsize=8)
    ax = axes[1]
    ax.plot(x, [100 * s["missing"] / s["expected"] if s["expected"] else 0 for s in steps], color=ORANGE, linewidth=2, marker="o", markersize=5)
    style(ax, "Deliveries missing", "% of expected", "busy repeaters")
    ax = axes[2]
    ax.plot(x, [s["db_queries_per_burst"] for s in steps], color=BLUE, linewidth=2, marker="o", markersize=5)
    style(ax, "DB statements per sent burst", "statements", "busy repeaters")
    fig.suptitle(f"Load ramp ({d.get('backend')})", x=0.01, ha="left", color=INK)
    fig.tight_layout()
    fig.savefig(os.path.join(d["_dir"], "ramp.png"), dpi=110, facecolor=SURFACE)
    plt.close(fig)


def plot_samples(plt, d):
    path = os.path.join(d["_dir"], d["area"] + ".samples.csv")
    if not os.path.exists(path):
        return
    rows = read_csv(path)
    if len(rows) < 2:
        return
    t = [r["t_s"] for r in rows]
    rate = [0] + [(rows[i]["received"] - rows[i - 1]["received"]) / max(t[i] - t[i - 1], 1e-9) for i in range(1, len(rows))]
    panels = [
        ("RSS", [r["rss_mb"] for r in rows], "MB"),
        ("Goroutines", [r["goroutines"] for r in rows], "count"),
        ("Open file descriptors", [r["fds"] for r in rows], "count"),
        ("DB connections in use", [r["db_in_use"] for r in rows], "count"),
        ("Call tracker queue", [r["tracker_queue"] for r in rows], "packets"),
        ("Bursts received by the targets", rate, "per second"),
    ]
    fig, axes = plt.subplots(2, 3, figsize=(15, 7), facecolor=SURFACE)
    for ax, (title, ys, unit) in zip(axes.flat, panels):
        ax.plot(t, ys, color=BLUE, linewidth=2)
        style(ax, title, unit, "seconds")
    fig.suptitle(f"{d['area']} ({d.get('backend')}): gauges sampled each second", x=0.01, ha="left", color=INK)
    fig.tight_layout()
    fig.savefig(os.path.join(d["_dir"], d["area"] + ".samples.png"), dpi=100, facecolor=SURFACE)
    plt.close(fig)


def plot_latency(plt, results, runs):
    by_backend = {}
    for d in runs:
        for name, s in (d.get("latency_ms") or {}).items():
            if s.get("count"):
                by_backend.setdefault(d.get("backend"), []).append((f"{d['area']}:{name}", s))
    for backend, items in by_backend.items():
        items = items[:24]
        fig, ax = plt.subplots(figsize=(12, 0.32 * len(items) + 1.5), facecolor=SURFACE)
        y = range(len(items))
        ax.barh([i + 0.2 for i in y], [max(s["p50"], 0.01) for _, s in items], height=0.36, color=BLUE, label="p50")
        ax.barh([i - 0.2 for i in y], [max(s["p99"], 0.01) for _, s in items], height=0.36, color=ORANGE, label="p99")
        ax.set_yticks(list(y))
        ax.set_yticklabels([n for n, _ in items], fontsize=8)
        ax.set_xscale("log")
        style(ax, f"Per-burst latency by area ({backend})", "", "ms (log)")
        ax.legend(frameon=False, fontsize=8)
        fig.tight_layout()
        fig.savefig(os.path.join(results, f"latency-{backend}.png"), dpi=110, facecolor=SURFACE)
        plt.close(fig)


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    results = sys.argv[1]
    runs = load(results)
    rows = summary(results, runs)
    for r in rows:
        print(f"{r['backend']:15} {r['area']:12} {'PASS' if r['passed'] else 'FAIL'} p99={r['p99_ms']} missing={r['missing']}")
    try:
        import matplotlib
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
    except ImportError:
        print("matplotlib is missing: no graphs")
        return
    for d in runs:
        plot_samples(plt, d)
        if d.get("area") == "ramp":
            plot_ramp(plt, d)
    plot_latency(plt, results, runs)


if __name__ == "__main__":
    main()
