#!/usr/bin/env python3
"""Render the pre-V4 task status and overlap matrix from the live queue."""

import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
QUEUE = ROOT / "automation" / "codex_task_queue.json"
OUTPUT = ROOT / "docs" / "research" / "BIRDTIE-V4-LEGACY-TASK-MATRIX.md"

OVERLAP = {
    "AUD": "BT-V4-AUD-002",
    "RUN": "BT-V4-TST-001",
    "DAT": "BT-V4-MIG-001",
    "AUT": "BT-V4-PIL-001",
    "ORG": "BT-V4-ORG-002",
    "ACT": "BT-V4-ACTY-001",
    "SRC": "BT-V4-OPP-001",
    "PUL": "BT-V4-NOW-001",
    "NOW": "BT-V4-NOW-001",
    "MAP": "BT-V4-MAP-002",
    "AGT": "BT-V4-INT-005",
    "RSV": "BT-V4-ACTN-001",
    "PLN": "BT-V4-PLN-001",
    "SAV": "BT-V4-ACTN-001",
    "DET": "BT-V4-ACTN-001",
    "NTF": "BT-V4-NOT-001",
    "INB": "BT-V4-CHT-002",
    "OAG": "BT-V4-ORG-001",
    "ANA": "BT-V4-ANA-001",
    "SAF": "BT-V4-SAF-003",
    "PER": "BT-V4-MAP-002",
    "TST": "BT-V4-TST-001",
    "REL": "BT-V4-PIL-003",
    "PIL": "BT-V4-PIL-002",
    "POL": "BT-V4-NOW-001",
    "COM": "BT-V4-COMM-001",
}

SPECIAL = {
    "BT-ORG-003": "BT-V4-ORG-003",
    "BT-MAP-004": "BT-V4-SAF-001",
    "BT-SAF-001": "BT-V4-SAF-001",
    "BT-REL-001": "BT-V4-PIL-003",
    "BT-COM-003": "BT-V4-ACTY-001",
    "BT-COM-006": "BT-V4-ACTY-002",
    "BT-COM-008": "BT-V4-ACTY-001",
    "BT-COM-010": "BT-V4-TST-001",
    "BT-COM-011": "BT-V4-E2E-003",
}


def main() -> None:
    data = json.loads(QUEUE.read_text(encoding="utf-8"))
    legacy = [task for task in data["tasks"] if not task["id"].startswith("BT-V4-")]
    if len(legacy) != 57:
        raise SystemExit(f"Expected 57 pre-V4 tasks, found {len(legacy)}")
    lines = [
        "# V4 对既有任务的逐项核验矩阵",
        "",
        "日期：2026-10-01。由 `automation/codex_task_queue.json` 生成；每行的验收证据全文及时间戳保留在 live queue。`V4 交叉项` 表示需要复用或扩展的关联，并不把旧 DONE 自动升级为 V4 DONE。旧任务均保留，不删除、不重置。",
        "",
        "| 既有 ID | Live 状态 | V4 交叉项 | 既有证据或未完成条件（摘要） |",
        "| --- | --- | --- | --- |",
    ]
    for task in legacy:
        task_id = task["id"]
        v4 = SPECIAL.get(task_id, OVERLAP[task_id.split("-")[1]])
        evidence = task.get("evidence") or task.get("blocked_reason")
        if evidence is None:
            evidence = "尚未领取；依赖/外部准入见 live queue。"
        evidence = " ".join(str(evidence).split()).replace("|", "/")
        if len(evidence) > 170:
            evidence = evidence[:167].rstrip() + "…"
        lines.append(f"| `{task_id}` | {task['status']} | `{v4}` | {evidence} |")
    lines.extend([
        "",
        "## 读法与边界",
        "",
        "- 旧 45 项 Functional MVP 工作与新增 12 项 Community 工作合计 57 项：51 DONE、3 TODO、3 BLOCKED。队列无 PARTIAL 状态枚举；功能差距的 PARTIAL 分类在产品差距分析中，不伪装成完成。",
        "- `BT-AUT-002`、`BT-TST-002`、`BT-REL-001` 的 P0 BLOCKED 原样保留。V4 额外的三项 `BT-V4-PIL-*` 是新阶段试点门槛，不应与旧任务重复报为已完成。",
        "- 旧 Connection/Chat 与 Intent/Place 数据结构可复用，V4 对应任务仍需按新增语义重新验收。旧 Community 和 Organization Activity 通过了本地合成数据/真机，不证明 Business 主体、跨城在线意图或真实 CSSA 试点。",
        "- 证据定位：`docs/reports/FUNCTIONAL-MVP-COMPLETION-REPORT.md`、`docs/testing/COMMUNITY-SOCIAL-PHYSICAL-DEVICE-2026-10-01.md`，以及 live queue 每项 `evidence`/`blocked_reason`。",
        "",
    ])
    OUTPUT.write_text("\n".join(lines), encoding="utf-8")
    print(f"Wrote {len(legacy)} legacy rows to {OUTPUT}")


if __name__ == "__main__":
    main()
