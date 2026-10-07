#!/usr/bin/env python3
"""Exercise taskctl against a disposable queue without mutating the live backlog."""

import hashlib
import json
import subprocess
import sys
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
TASKCTL = ROOT / "automation" / "taskctl.py"
LIVE = ROOT / "automation" / "codex_task_queue.json"


def call(queue: Path, *args: str, expected: int = 0) -> str:
    result = subprocess.run(
        [sys.executable, str(TASKCTL), "--queue", str(queue), *args],
        capture_output=True, text=True, check=False,
    )
    if result.returncode != expected:
        raise AssertionError(f"{args}: exit {result.returncode}: {result.stdout}{result.stderr}")
    return result.stdout + result.stderr


def main() -> None:
    before = hashlib.sha256(LIVE.read_bytes()).hexdigest()
    with tempfile.TemporaryDirectory(prefix="birdtie-taskctl-") as directory:
        queue = Path(directory) / "queue.json"
        queue.write_text(json.dumps({"version": 1, "tasks": [
            {"id": "SAFE-A", "priority": "P0", "status": "TODO", "depends_on": []},
            {"id": "SAFE-B", "priority": "P0", "status": "TODO", "depends_on": ["SAFE-A"]},
            {"id": "SAFE-C", "priority": "P1", "status": "TODO", "depends_on": ["SAFE-B"]},
        ]}), encoding="utf-8")
        assert "3 tasks" in call(queue, "validate")
        assert "TOTAL: 3" in call(queue, "summary")
        assert '"id": "SAFE-A"' in call(queue, "next")
        assert "Started SAFE-A" in call(queue, "start", "SAFE-A")
        assert "Finish or block" in call(queue, "next")
        assert "Completed SAFE-A" in call(queue, "done", "SAFE-A", "--evidence", "disposable check")
        assert '"id": "SAFE-B"' in call(queue, "next")
        assert "Started SAFE-B" in call(queue, "start", "SAFE-B")
        assert "Partial SAFE-B" in call(queue, "partial", "SAFE-B", "--reason", "remaining acceptance")
        assert "Started SAFE-B" in call(queue, "start", "SAFE-B")
        assert "Blocked SAFE-B" in call(queue, "block", "SAFE-B", "--reason", "disposable blocker")
        assert "Started SAFE-B" in call(queue, "start", "SAFE-B")
        assert "Completed SAFE-B" in call(queue, "done", "SAFE-B", "--evidence", "disposable check")
        assert '"id": "SAFE-C"' in call(queue, "next")
        assert "Started SAFE-C" in call(queue, "start", "SAFE-C")
        assert "Completed SAFE-C" in call(queue, "done", "SAFE-C", "--evidence", "disposable check")
        assert "No executable TODO" in call(queue, "next")
        assert "DONE=2" in call(queue, "summary")  # P0 subset
        broken = json.loads(queue.read_text(encoding="utf-8"))
        broken["tasks"][0]["evidence"] = ""
        queue.write_text(json.dumps(broken), encoding="utf-8")
        assert "DONE without evidence" in call(queue, "validate", expected=1)
    after = hashlib.sha256(LIVE.read_bytes()).hexdigest()
    if before != after:
        raise AssertionError("Live queue changed during disposable taskctl check")
    print("PASS: summary/next/start/done/block/partial/validate, dependency order, evidence guard, live queue unchanged")


if __name__ == "__main__":
    main()
