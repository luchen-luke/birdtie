#!/usr/bin/env python3
"""One-time, idempotent import of the user-provided V4 workbook into the live queue.

Existing task records and statuses are never overwritten. This importer does not
promote a planning status to DONE; the repository workflow owns live evidence.
"""

import argparse
import io
import json
import zipfile
from datetime import datetime, timezone
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("package", type=Path)
    parser.add_argument("--queue", type=Path, default=Path(__file__).with_name("codex_task_queue.json"))
    args = parser.parse_args()

    # The supplied package is an XLSX planning source. The importer runs with
    # the bundled workspace Python, which includes openpyxl for read-only work.
    from openpyxl import load_workbook

    with zipfile.ZipFile(args.package) as package:
        source = io.BytesIO(package.read("BirdTie_TAPD_Backlog_V4.xlsx"))
    rows = list(load_workbook(source, read_only=True, data_only=True)["Requirements"].values)
    header = rows[0]
    expected = ("#", "ID", "Phase", "Epic", "Priority", "Status", "Title")
    if tuple(header[: len(expected)]) != expected:
        raise SystemExit("Unexpected V4 Requirements column layout")

    queue = json.loads(args.queue.read_text(encoding="utf-8"))
    existing = {task["id"] for task in queue["tasks"]}
    source_ids = {row[1] for row in rows[1:] if row and isinstance(row[1], str) and row[1].startswith("BT-V4-")}
    if len(source_ids) != 87:
        raise SystemExit(f"Expected 87 unique V4 tasks, found {len(source_ids)}")
    additions = []
    for row in rows[1:]:
        if not row or row[1] not in source_ids or row[1] in existing:
            continue
        dependencies = [item.strip() for item in str(row[9] or "").split(",") if item.strip() and item.strip() != "-"]
        if any(dependency not in source_ids for dependency in dependencies):
            raise SystemExit(f"Unknown dependency for {row[1]}")
        status = "BLOCKED" if row[5] == "BLOCKED_EXTERNAL" else "TODO"
        if row[5] not in {"TODO", "BLOCKED_EXTERNAL"}:
            raise SystemExit(f"Unexpected planning status {row[5]} for {row[1]}")
        task = {
            "id": row[1],
            "epic": row[3],
            "priority": row[4],
            "phase": row[2],
            "gate": row[12],
            "title": row[6],
            "depends_on": dependencies,
            "status": status,
            "goal": row[7],
            "baseline": row[8],
            "acceptance": [row[10]],
            "verify": [row[11]],
            "source": "BirdTie_V4_Execution_Package.zip!/BirdTie_TAPD_Backlog_V4.xlsx#Requirements",
        }
        if status == "BLOCKED":
            task["blocked_reason"] = "规划阶段标记 BLOCKED_EXTERNAL；须先满足依赖，并取得真实身份/合作/部署证据后重新审计。"
        additions.append(task)
    queue["tasks"].extend(additions)
    queue["updated"] = datetime.now(timezone.utc).isoformat()
    args.queue.write_text(json.dumps(queue, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Imported {len(additions)} V4 tasks; preserved {len(existing)} existing tasks")


if __name__ == "__main__":
    main()
