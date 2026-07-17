#!/usr/bin/env python3
# Generates the consolidated metrics catalog (configs/metrics.yaml) from
# docs/CATMonitor_indi_list.md, and copies it to features/web/metrics.yaml and
# features/health/metrics.yaml (per-module editable catalogs).
import os, re, shutil

ROOT = "/mnt/d/AICoding/jw/CATMonitor"
INDI = os.path.join(ROOT, "docs/CATMonitor_indi_list.md")
SECTIONS = [("cpu","## 1. CPU"),("memory","## 2. Memory"),("disk","## 3. Disk"),
            ("gpu","## 4. GPU"),("npu","## 5. NPU"),("network","## 6. Network")]
INTERVAL = {"cpu":"3s","memory":"3s","disk":"5s","gpu":"3s","npu":"3s","network":"3s"}
STATIC = {
  "cpu":{"model_info","numa_node_num","core_num","die_core_num","numa_core_num","cpu_num","min_freq","max_freq","l1d_cache_size","l1i_cache_size","l2_cache_size","l3_cache_size"},
  "memory":{"module_info","module_size","module_num"},
  "npu":{"npu_num","chip_type","driver_version","comm_topo","hbm_total_memory","aicore_rated_freq"},
  "disk":set(),"gpu":set(),"network":set(),
}
EXTRA = {
  "memory":[("usage_detail","内存明细","Medium","")],
  "disk":[("space_detail","空间明细","Medium","")],
  "network":[("rx_bytes_total","接收字节","Medium","B"),("tx_bytes_total","发送字节","Medium","B"),("interface_status","接口状态","Medium","")],
  "cpu":[],"gpu":[],"npu":[],
}
def parse(text):
    out=[]
    for line in text.splitlines():
        line=line.strip()
        if not line.startswith("|") or "----" in line: continue
        cols=[c.strip() for c in line.strip("|").split("|")]
        if len(cols)<7 or not re.match(r"^\d+\.\d+$",cols[0]): continue
        out.append((cols[1],cols[2],cols[3],cols[6]))
    return out
def main():
    doc=open(INDI,encoding="utf-8").read()
    positions=[(c, doc.find(h)) for c,h in SECTIONS]
    lines=["components:"]
    for k,(comp,start) in enumerate(positions):
        end=positions[k+1][1] if k+1<len(positions) else len(doc)
        rows=parse(doc[start:end])
        specs=[]; seen=set()
        for r in rows:
            if r[0] in seen: continue
            seen.add(r[0]); specs.append(r)
        for r in EXTRA[comp]:
            if r[0] in seen: continue
            seen.add(r[0]); specs.append(r)
        lines.append(f"  - component: {comp}")
        lines.append(f"    interval: {INTERVAL[comp]}")
        lines.append("    metrics:")
        for name,cn,prio,unit in specs:
            st="true" if name in STATIC[comp] else "false"
            cnq='"'+cn.replace('"','\\"')+'"'
            unitq='"'+unit.replace('"','\\"')+'"' if unit else '""'
            lines.append(f"      - name: {name}")
            lines.append(f"        cn_name: {cnq}")
            lines.append(f"        priority: {prio}")
            lines.append(f"        unit: {unitq}")
            lines.append(f"        static: {st}")
        print(f"{comp}: {len(specs)} metrics")
    out="\n".join(lines)+"\n"
    cfg=os.path.join(ROOT,"configs","metrics.yaml")
    open(cfg,"w",encoding="utf-8").write(out)
    print(f"-> {cfg}")
    for mod in ("web","health"):
        dst=os.path.join(ROOT,"features",mod,"metrics.yaml")
        shutil.copyfile(cfg,dst)
        print(f"-> {dst} (copy)")
main()
