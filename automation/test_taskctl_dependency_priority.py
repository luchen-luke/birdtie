"""Task scheduling regressions on isolated queues; no live queue mutations."""
import contextlib
import copy
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parent))
import taskctl


def fixture_task(task_id, priority="P0", status="TODO", dependencies=(), **extras):
    task = {
        "id": task_id,
        "title": "Isolated scheduling fixture",
        "priority": priority,
        "status": status,
        "depends_on": list(dependencies),
        "phase": "original-phase",
        "owner": "original-owner",
        "lease": {"holder": "original-worker"},
    }
    if status == "DONE":
        task["evidence"] = "isolated fixture evidence only"
    if status == "BLOCKED":
        task["blocked_reason"] = "External condition remains missing"
    if status == "PARTIAL":
        task["partial_reason"] = "Actual implementation remains partial"
    task.update(extras)
    return task


def next_task(data):
    taskctl.validate(data)
    stream = io.StringIO()
    with contextlib.redirect_stdout(stream):
        taskctl.cmd_next(data)
    output = stream.getvalue()
    if "{" not in output:
        return None
    return json.loads(output[output.index("{"):])


class DependencyPriorityTests(unittest.TestCase):
    def blocked_p0_fixture(self, prerequisite, **root_extras):
        return {"tasks": [
            fixture_task("UNRELATED-P0"),
            fixture_task("WAITING-P0", dependencies=(prerequisite["id"],), **root_extras),
            prerequisite,
        ]}

    def assert_no_boost(self, data):
        before = copy.deepcopy(data)
        self.assertEqual(next_task(data)["id"], "UNRELATED-P0")
        self.assertEqual(data, before)

    def test_ready_p1_required_by_p0_precedes_unrelated_ready_p0(self):
        data = self.blocked_p0_fixture(fixture_task("NECESSARY-P1", priority="P1"))
        before = copy.deepcopy(data)
        selected = next_task(data)
        self.assertEqual(selected["id"], "NECESSARY-P1")
        self.assertEqual(selected["priority"], "P1")
        self.assertEqual(selected["phase"], "original-phase")
        self.assertEqual(selected["owner"], "original-owner")
        self.assertEqual(selected["lease"], {"holder": "original-worker"})
        self.assertEqual(data, before)

    def test_current_in_progress_remains_absolute_priority(self):
        data = self.blocked_p0_fixture(fixture_task("NECESSARY-P1", priority="P1"))
        data["tasks"].append(fixture_task("CURRENT-P2", priority="P2", status="IN_PROGRESS"))
        before = copy.deepcopy(data)
        self.assertEqual(next_task(data)["id"], "CURRENT-P2")
        self.assertEqual(data, before)

    def test_without_cross_priority_dependency_legacy_priority_index_order_is_unchanged(self):
        data = {"tasks": [
            fixture_task("FIRST-P1", priority="P1"),
            fixture_task("FIRST-P0"),
            fixture_task("SECOND-P0"),
            fixture_task("FIRST-P2", priority="P2"),
            fixture_task("SECOND-P1", priority="P1"),
        ]}
        expected = ["FIRST-P0", "SECOND-P0", "FIRST-P1", "SECOND-P1", "FIRST-P2"]
        actual = []
        while selected := next_task(data):
            actual.append(selected["id"])
            taskctl.get_task(data, selected["id"]).update(status="DONE", evidence="isolated ordering fixture")
        self.assertEqual(actual, expected)

    def test_equal_inherited_priority_keeps_original_index(self):
        data = {"tasks": [
            fixture_task("UNRELATED-P0"),
            fixture_task("P1-EARLY", priority="P1"),
            fixture_task("WAITING-P0", dependencies=("P1-LATE", "P1-EARLY")),
            fixture_task("P1-LATE", priority="P1"),
        ]}
        self.assertEqual(next_task(data)["id"], "P1-EARLY")

    def test_ready_ancestor_only_when_all_its_dependencies_are_done(self):
        data = {"tasks": [
            fixture_task("UNRELATED-P0"),
            fixture_task("WAITING-P0", dependencies=("P1-A",)),
            fixture_task("P1-A", priority="P1", dependencies=("P1-B",)),
            fixture_task("P1-B", priority="P1", dependencies=("ALREADY-DONE",)),
            fixture_task("ALREADY-DONE", status="DONE"),
        ]}
        self.assertEqual(next_task(data)["id"], "P1-B")
        taskctl.get_task(data, "P1-B").update(status="DONE", evidence="isolated necessary prerequisite")
        self.assertEqual(next_task(data)["id"], "P1-A")
        self.assertEqual(taskctl.get_task(data, "WAITING-P0")["status"], "TODO")

    def test_finite_queue_executes_necessary_p1_chain_without_waiting_all_other_p0(self):
        data = {"tasks": [fixture_task(f"UNRELATED-P0-{number:02d}") for number in range(40)]}
        data["tasks"] += [
            fixture_task("WAITING-P0", dependencies=("P1-A",)),
            fixture_task("P1-A", priority="P1", dependencies=("P1-B",)),
            fixture_task("P1-B", priority="P1"),
        ]
        actual = []
        while selected := next_task(data):
            task = taskctl.get_task(data, selected["id"])
            self.assertTrue(taskctl.deps_done(task, taskctl.task_map(data)))
            actual.append(task["id"])
            task.update(status="DONE", evidence="isolated finite scheduling fixture")
        self.assertEqual(actual[:2], ["P1-B", "P1-A"])
        self.assertEqual(len(actual), 43)
        self.assertEqual(len(set(actual)), 43)
        self.assertIn("WAITING-P0", actual)

    def test_blocked_or_partial_anywhere_in_closure_prevents_inherited_priority(self):
        for status in ("BLOCKED", "PARTIAL"):
            with self.subTest(status=status):
                data = self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1"))
                taskctl.get_task(data, "WAITING-P0")["depends_on"].append("EXTERNAL")
                data["tasks"].append(fixture_task("EXTERNAL", status=status))
                self.assert_no_boost(data)

    def test_a_blocked_or_partial_p0_does_not_promote_its_prerequisite(self):
        for status in ("BLOCKED", "PARTIAL"):
            with self.subTest(status=status):
                data = {"tasks": [
                    fixture_task("UNRELATED-P0"),
                    fixture_task("WAITING-P0", status=status, dependencies=("P1-READY",)),
                    fixture_task("P1-READY", priority="P1"),
                ]}
                self.assert_no_boost(data)

    def test_live_only_or_post_pilot_or_unknown_gate_is_not_promoted(self):
        extras = (
            {"completion_scope": "LIVE_ONLY"},
            {"gate": "Post-Pilot"},
            {"release_stage": "V4_POST_PILOT"},
            {"gate": "Unknown future gate"},
            {"release_stage": "Unknown future stage"},
        )
        for fields in extras:
            with self.subTest(fields=fields):
                self.assert_no_boost(self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1", **fields)))
                self.assert_no_boost(self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1"), **fields))

    def test_a_gated_sibling_blocks_entire_root_but_not_an_independent_root(self):
        data = self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1"))
        taskctl.get_task(data, "WAITING-P0")["depends_on"].append("POST-PILOT")
        data["tasks"].append(fixture_task("POST-PILOT", priority="P2", gate="Post-Pilot"))
        self.assert_no_boost(data)
        data["tasks"].append(fixture_task("INDEPENDENT-P0", dependencies=("P1-READY",)))
        self.assertEqual(next_task(data)["id"], "P1-READY")

    def test_known_gates_and_missing_gate_allow_ordering_but_do_not_mutate_release_state(self):
        for gate in (None, "", "Foundation", "Social Alpha", "Beta", "Business Pilot", "Aberdeen Closed Pilot"):
            with self.subTest(gate=gate):
                data = self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1", gate=gate))
                data["closed_pilot_ready"] = "NO"
                data["a2a_enabled"] = False
                before = copy.deepcopy(data)
                self.assertEqual(next_task(data)["id"], "P1-READY")
                self.assertEqual(data, before)

    def test_unknown_priority_in_closure_is_not_promoted(self):
        for priority in (None, "P9", "UNKNOWN", "p1"):
            with self.subTest(priority=priority):
                data = self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1"))
                data["tasks"].append(fixture_task("UNKNOWN-PRIORITY", priority=priority))
                taskctl.get_task(data, "WAITING-P0")["depends_on"].append("UNKNOWN-PRIORITY")
                self.assert_no_boost(data)

    def test_p2_or_p3_is_not_promoted_even_if_required_by_p0(self):
        for priority in ("P2", "P3"):
            with self.subTest(priority=priority):
                self.assert_no_boost(self.blocked_p0_fixture(fixture_task("LOWER-PRIORITY", priority=priority)))

    def test_existing_done_dependency_with_old_gates_is_still_done(self):
        data = self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1", dependencies=("OLD-DONE",)))
        data["tasks"].append(fixture_task("OLD-DONE", status="DONE", gate="Unknown legacy classification"))
        self.assertEqual(next_task(data)["id"], "P1-READY")

    def test_ready_p1_that_is_not_a_p0_prerequisite_is_not_promoted(self):
        data = {"tasks": [
            fixture_task("UNRELATED-P0"),
            fixture_task("P1-READY", priority="P1"),
            fixture_task("P2-WAITING", priority="P2", dependencies=("P1-READY",)),
        ]}
        self.assert_no_boost(data)

    def test_only_blocked_or_unmet_tasks_produce_no_executable_todo(self):
        data = {"tasks": [
            fixture_task("EXTERNAL", status="BLOCKED"),
            fixture_task("UNMET", dependencies=("EXTERNAL",)),
        ]}
        before = copy.deepcopy(data)
        self.assertIsNone(next_task(data))
        self.assertEqual(data, before)

    def test_start_unmet_task_still_fails_and_does_not_write_queue(self):
        data = self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1"))
        with tempfile.TemporaryDirectory(prefix="birdtie-taskctl-start-") as directory:
            queue = Path(directory) / "queue.json"
            queue.write_text(json.dumps(data), encoding="utf-8")
            before = queue.read_bytes()
            with self.assertRaisesRegex(SystemExit, "Dependencies not DONE"):
                taskctl.cmd_start(queue, data, "WAITING-P0")
            self.assertEqual(queue.read_bytes(), before)
            self.assertEqual(taskctl.get_task(data, "WAITING-P0")["status"], "TODO")

    def test_an_active_task_still_prevents_starting_even_promoted_ready_p1(self):
        data = self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1"))
        data["tasks"].append(fixture_task("CURRENT", status="IN_PROGRESS"))
        with tempfile.TemporaryDirectory(prefix="birdtie-taskctl-active-") as directory:
            queue = Path(directory) / "queue.json"
            queue.write_text(json.dumps(data), encoding="utf-8")
            before = queue.read_bytes()
            with self.assertRaisesRegex(SystemExit, "Task already IN_PROGRESS"):
                taskctl.cmd_start(queue, data, "P1-READY")
            self.assertEqual(queue.read_bytes(), before)

    def test_invalid_unknown_dependency_and_cycle_still_fail_native_validation(self):
        data = {"tasks": [fixture_task("A", dependencies=("UNKNOWN",))]}
        with self.assertRaisesRegex(SystemExit, "Invalid dependency"):
            next_task(data)
        data = {"tasks": [fixture_task("A", dependencies=("B",)), fixture_task("B", dependencies=("A",))]}
        with self.assertRaisesRegex(SystemExit, "Dependency cycle"):
            next_task(data)

    def test_cli_reads_disposable_queue_and_preserves_json_schema_and_bytes(self):
        data = self.blocked_p0_fixture(fixture_task("P1-READY", priority="P1"))
        with tempfile.TemporaryDirectory(prefix="birdtie-taskctl-cli-") as directory:
            queue = Path(directory) / "queue.json"
            queue.write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")
            before = queue.read_bytes()
            result = subprocess.run(
                [sys.executable, "-B", str(Path(taskctl.__file__)), "--queue", str(queue), "next"],
                env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1", PYTHONIOENCODING="utf-8"),
                encoding="utf-8", capture_output=True, check=False, timeout=30,
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(json.loads(result.stdout), taskctl.get_task(data, "P1-READY"))
            self.assertEqual(queue.read_bytes(), before)


if __name__ == "__main__":
    unittest.main(verbosity=2)
