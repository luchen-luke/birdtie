"""Controlled parallel scheduling regressions on isolated queue copies only."""
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
from test_taskctl_dependency_priority import fixture_task


class ParallelQueueTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="birdtie-taskctl-parallel-")
        self.addCleanup(self.temporary.cleanup)
        self.queue = Path(self.temporary.name) / "queue.json"
        self.original = {"version": 1, "closed_pilot_ready": "NO", "archive": [{"id": "old"}], "tasks": [
            fixture_task("CURRENT", status="IN_PROGRESS"),
            fixture_task("DONE", status="DONE"),
            fixture_task("B", dependencies=("DONE",)),
            fixture_task("C"), fixture_task("D"),
        ]}
        self.queue.write_text(json.dumps(self.original, ensure_ascii=False), encoding="utf-8")

    def invoke(self, function, *arguments, **keywords):
        data = taskctl.load(self.queue)
        with contextlib.redirect_stdout(io.StringIO()):
            function(self.queue, data, *arguments, **keywords)
        return taskctl.load(self.queue)

    def configure(self, limit=3):
        return self.invoke(taskctl.cmd_parallel_config, "root", limit)

    def lease_current(self, scope="apps/api/internal/agentprofile"):
        return self.invoke(taskctl.cmd_lease, "CURRENT", "root", [scope], "root")

    def prepare(self, limit=3):
        self.configure(limit)
        return self.lease_current()

    def start(self, task_id="B", owner="worker-b", scopes=None, coordinator="root"):
        return self.invoke(taskctl.cmd_start, task_id, parallel=True, owner=owner,
                           scopes=scopes or ["apps/client/lib/src/parallel_b.dart"], coordinator=coordinator)

    def assert_rejected_unchanged(self, function, *arguments, **keywords):
        before = self.queue.read_bytes()
        with self.assertRaises(SystemExit):
            self.invoke(function, *arguments, **keywords)
        self.assertEqual(self.queue.read_bytes(), before)
        self.assertFalse(list(self.queue.parent.glob("queue.json.*.tmp")))

    def replace(self, data):
        self.queue.write_text(json.dumps(data, ensure_ascii=False), encoding="utf-8")

    def cli(self, *arguments):
        return subprocess.run([sys.executable, "-B", str(Path(taskctl.__file__)), "--queue", str(self.queue),
                               *arguments], capture_output=True, encoding="utf-8", timeout=30,
                              env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1", PYTHONIOENCODING="utf-8"))

    def test_legacy_validation_still_rejects_multiple_active_without_opt_in(self):
        data = copy.deepcopy(self.original)
        taskctl.get_task(data, "B")["status"] = "IN_PROGRESS"
        with self.assertRaisesRegex(SystemExit, "Multiple IN_PROGRESS"):
            taskctl.validate(data)

    def test_legacy_start_with_active_and_parallel_arguments_without_config_are_rejected(self):
        self.assert_rejected_unchanged(taskctl.cmd_start, "B")
        self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="worker-b",
                                      scopes=["apps/client/lib/src/parallel_b.dart"], coordinator="root")

    def test_config_and_existing_lease_preserve_all_old_task_objects_and_root_history(self):
        self.configure()
        data = self.lease_current()
        self.assertEqual(data["tasks"], self.original["tasks"])
        self.assertEqual(data["archive"], self.original["archive"])
        self.assertEqual(data["closed_pilot_ready"], "NO")
        self.assertEqual(data["parallel_execution"]["limit"], 3)
        self.assertEqual(data["parallel_execution"]["leases"]["CURRENT"]["write_scope"],
                         ["apps/api/internal/agentprofile"])

    def test_config_limits_and_coordinator_replacement_fail_without_writing(self):
        for limit in (0, 4, -1, True, "3"):
            with self.subTest(limit=limit):
                self.assert_rejected_unchanged(taskctl.cmd_parallel_config, "root", limit)
        self.configure()
        self.assert_rejected_unchanged(taskctl.cmd_parallel_config, "another-writer", 3)

    def test_parallel_start_requires_existing_active_lease(self):
        self.configure()
        self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="worker-b",
                                      scopes=["apps/client/lib/src/parallel_b.dart"], coordinator="root")

    def test_only_existing_active_task_can_get_lease_and_it_cannot_be_reassigned(self):
        self.configure()
        for task_id in ("B", "DONE", "UNKNOWN"):
            with self.subTest(task=task_id):
                self.assert_rejected_unchanged(taskctl.cmd_lease, task_id, "worker-b", ["docs/research"], "root")
        self.lease_current()
        self.assert_rejected_unchanged(taskctl.cmd_lease, "CURRENT", "another-owner", ["docs/research"], "root")

    def test_three_disjoint_active_tasks_allowed_fourth_and_limit_decrease_rejected(self):
        self.prepare()
        self.start()
        data = self.start("C", "worker-c", ["docs/research/parallel_c.md"])
        self.assertEqual(sum(task["status"] == "IN_PROGRESS" for task in data["tasks"]), 3)
        self.assertEqual(set(data["parallel_execution"]["leases"]), {"CURRENT", "B", "C"})
        self.assert_rejected_unchanged(taskctl.cmd_start, "D", parallel=True, owner="worker-d",
                                      scopes=["automation/new_d.py"], coordinator="root")
        self.assert_rejected_unchanged(taskctl.cmd_parallel_config, "root", 2)

    def test_duplicate_owner_is_case_insensitive(self):
        self.prepare()
        self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="ROOT",
                                      scopes=["docs/research/parallel_b.md"], coordinator="root")
        self.start()
        self.assert_rejected_unchanged(taskctl.cmd_start, "C", parallel=True, owner="WORKER-B",
                                      scopes=["docs/research/parallel_c.md"], coordinator="root")

    def test_scopes_are_case_insensitive_normalized_and_component_based(self):
        self.configure()
        self.lease_current("apps\\api\\internal\\abc")
        data = self.start(scopes=["apps/api/internal/abcd"])
        self.assertEqual(data["parallel_execution"]["leases"]["B"]["write_scope"], ["apps/api/internal/abcd"])
        self.assert_rejected_unchanged(taskctl.cmd_start, "C", parallel=True, owner="worker-c",
                                      scopes=["APPS\\API\\INTERNAL\\AbCd\\file.go"], coordinator="root")

    def test_scope_parent_child_and_equal_are_rejected_in_both_directions(self):
        self.prepare()
        for scope in ("apps/api/internal/agentprofile", "apps/api/internal", "apps/api/internal/agentprofile/private.go"):
            with self.subTest(scope=scope):
                self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="worker-b",
                                              scopes=[scope], coordinator="root")

    def test_invalid_empty_unknown_traversal_root_outside_and_wildcard_scopes_are_rejected(self):
        self.prepare()
        outside = str(self.queue.parent / "outside.py")
        for scopes in ([], [""], [" "], ["."], [str(taskctl.ROOT)], ["unknown-repository-area/file.go"],
                       ["apps/../docs"], ["../birdtie/apps"], [outside], ["apps/*"], ["apps/?"],
                       ["apps/[ab]"], ["apps/{a,b}"], ["apps\n/api"], ["D:relative"]):
            with self.subTest(scopes=scopes):
                self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="worker-b",
                                              scopes=scopes, coordinator="root")

    def test_absolute_inside_repo_is_normalized_and_multiple_scopes_are_supported(self):
        self.prepare()
        data = self.start(scopes=[str(taskctl.ROOT / "docs/research/parallel_b.md"), "automation/new_b.py"])
        self.assertEqual(data["parallel_execution"]["leases"]["B"]["write_scope"],
                         ["automation/new_b.py", "docs/research/parallel_b.md"])

    def test_duplicate_or_internally_overlapping_scope_parameters_are_rejected(self):
        self.prepare()
        for scopes in (["docs/research", "DOCS\\RESEARCH"], ["docs/research", "docs/research/file.md"]):
            with self.subTest(scopes=scopes):
                self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="worker-b",
                                              scopes=scopes, coordinator="root")

    def test_worker_cannot_claim_queue_or_parent_directory_write_scope(self):
        self.prepare()
        for scope in ("automation", "automation/codex_task_queue.json"):
            with self.subTest(scope=scope):
                self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="worker-b",
                                              scopes=[scope], coordinator="root")

    def test_parallel_dependency_must_be_done_and_only_selected_task_object_changes(self):
        data = copy.deepcopy(self.original)
        taskctl.get_task(data, "B")["depends_on"] = ["C"]
        self.replace(data)
        self.prepare()
        self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="worker-b",
                                      scopes=["docs/research/b.md"], coordinator="root")
        before = taskctl.load(self.queue)
        after = self.start("C", "worker-c", ["docs/research/c.md"])
        self.assertEqual([task for task in before["tasks"] if task["id"] != "C"],
                         [task for task in after["tasks"] if task["id"] != "C"])

    def test_parallel_cannot_restart_blocked_partial_done_or_clear_external_gates(self):
        for status in ("BLOCKED", "PARTIAL", "DONE", "IN_PROGRESS"):
            with self.subTest(status=status):
                data = copy.deepcopy(self.original)
                if status == "IN_PROGRESS":
                    taskctl.get_task(data, "CURRENT")["status"] = "TODO"
                data["tasks"][2] = fixture_task("B", status=status)
                self.replace(data)
                self.configure()
                self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="worker-b",
                                              scopes=["docs/research/b.md"], coordinator="root")

    def test_live_external_unknown_completion_gate_release_stage_and_unknown_priority_are_rejected(self):
        forbidden = ({"completion_scope": "LIVE_ONLY"}, {"completion_scope": "UNKNOWN"},
                     {"completion_scope": None}, {"completion_scope": []}, {"completion_scope": {}},
                     {"completion_scope": ""}, {"external_gate_ids": None}, {"activation_gates": None},
                     {"completion_scope": "CODE_AND_LOCAL_VERIFICATION", "external_gate_ids": ["PROVIDER_APPROVAL"]},
                     {"external_gate_ids": ["AGE_WRITE_CONTRACT"]}, {"activation_gates": ["approved transport"]},
                     {"gate": "Post-Pilot"}, {"gate": "unknown-release-gate"},
                     {"release_stage": "V4_POST_PILOT"}, {"priority": "P9"},
                     {"blocked_reason": "BLOCKED_EXTERNAL"}, {"partial_reason": "human review required"})
        for fields in forbidden:
            with self.subTest(fields=fields):
                data = copy.deepcopy(self.original)
                taskctl.get_task(data, "B").update(fields)
                self.replace(data)
                self.prepare()
                self.assert_rejected_unchanged(taskctl.cmd_start, "B", parallel=True, owner="worker-b",
                                              scopes=["docs/research/b.md"], coordinator="root")

    def test_parallel_next_is_readonly_fair_and_only_lists_ready_local_tasks(self):
        data = {"tasks": [fixture_task("UNRELATED-P0"), fixture_task("P1", priority="P1"),
                          fixture_task("WAITING-P0", dependencies=("P1",)),
                          fixture_task("LIVE", completion_scope="LIVE_ONLY"),
                          fixture_task("EXT", external_gate_ids=["PROVIDER_APPROVAL"]),
                          fixture_task("GATED", gate="unknown"),
                          fixture_task("CURRENT", status="IN_PROGRESS")]}
        self.replace(data)
        before = self.queue.read_bytes()
        result = self.cli("next", "--parallel")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        response = json.loads(result.stdout)
        self.assertEqual([task["id"] for task in response["candidates"]], ["P1", "UNRELATED-P0"])
        self.assertEqual(response["unleased_active"], ["CURRENT"])
        self.assertEqual(self.queue.read_bytes(), before)

    def test_external_p0_root_does_not_boost_parallel_prerequisite(self):
        data = {"tasks": [fixture_task("UNRELATED-P0"), fixture_task("P1", priority="P1"),
                          fixture_task("EXTERNAL-P0", dependencies=("P1",), external_gate_ids=["HUMAN"])]}
        self.assertEqual([task["id"] for task in taskctl.ordered_candidates(data, parallel=True)],
                         ["UNRELATED-P0", "P1"])

    def test_parallel_coordinator_required_for_every_mutating_command(self):
        self.prepare()
        self.start()
        attempts = ((taskctl.cmd_done, ("B", "evidence")), (taskctl.cmd_block, ("B", "reason")),
                    (taskctl.cmd_partial, ("B", "reason")), (taskctl.cmd_reset, ("B",)),
                    (taskctl.cmd_parallel_config, ("worker-b", 3)))
        for function, arguments in attempts:
            with self.subTest(command=function.__name__):
                self.assert_rejected_unchanged(function, *arguments)

    def test_done_block_partial_and_reset_release_only_own_lease_append_history_and_preserve_archive(self):
        for function, final_status, extra in ((taskctl.cmd_done, "DONE", "actual isolated evidence"),
                                              (taskctl.cmd_block, "BLOCKED", "remaining external requirement"),
                                              (taskctl.cmd_partial, "PARTIAL", "remaining work"),
                                              (taskctl.cmd_reset, "TODO", None)):
            with self.subTest(status=final_status):
                self.replace(self.original)
                self.prepare()
                self.start()
                before = taskctl.load(self.queue)
                arguments = ("B", extra) if extra is not None else ("B",)
                after = self.invoke(function, *arguments, coordinator="root")
                self.assertEqual(taskctl.get_task(after, "B")["status"], final_status)
                self.assertEqual(set(after["parallel_execution"]["leases"]), {"CURRENT"})
                history = after["parallel_execution"]["lease_history"]
                self.assertEqual(len(history), 1)
                self.assertEqual(history[0]["task_id"], "B")
                self.assertEqual(history[0]["status"], final_status)
                self.assertEqual(history[0]["write_scope"], before["parallel_execution"]["leases"]["B"]["write_scope"])
                self.assertEqual(after["archive"], self.original["archive"])
                self.assertEqual([task for task in before["tasks"] if task["id"] != "B"],
                                 [task for task in after["tasks"] if task["id"] != "B"])

    def test_failure_evidence_or_reason_does_not_release_lease_or_change_queue(self):
        self.prepare()
        self.start()
        for function in (taskctl.cmd_done, taskctl.cmd_block, taskctl.cmd_partial):
            with self.subTest(function=function.__name__):
                self.assert_rejected_unchanged(function, "B", " ", coordinator="root")

    def test_stale_reader_cannot_overwrite_and_original_in_memory_data_remains_unchanged(self):
        self.prepare()
        stale = taskctl.load(self.queue)
        before_memory = copy.deepcopy(stale)
        self.start()
        before_file = self.queue.read_bytes()
        with self.assertRaisesRegex(SystemExit, "Queue changed after reading"):
            with contextlib.redirect_stdout(io.StringIO()):
                taskctl.cmd_block(self.queue, stale, "CURRENT", "isolated stale read", coordinator="root")
        self.assertEqual(self.queue.read_bytes(), before_file)
        self.assertEqual(stale, before_memory)

    def test_persistent_writer_lock_rejects_second_writer_without_queue_changes(self):
        before = self.queue.read_bytes()
        with taskctl.writer_lock(self.queue):
            result = self.cli("parallel-config", "--coordinator", "root", "--limit", "3")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Queue writer lock is held", result.stderr)
        self.assertEqual(self.queue.read_bytes(), before)
        self.assertTrue(self.queue.with_name("queue.json.lock").exists())
        self.configure()

    def test_forged_parallel_metadata_cannot_bypass_limit_lease_owner_scope_or_dependency_guards(self):
        self.prepare()
        self.start()
        valid = taskctl.load(self.queue)
        mutations = (
            lambda value: value.update(parallel_execution=None),
            lambda value: value["parallel_execution"].update(limit=4),
            lambda value: value["parallel_execution"]["leases"].pop("CURRENT"),
            lambda value: value["parallel_execution"]["leases"]["B"].update(owner="root"),
            lambda value: value["parallel_execution"]["leases"]["B"].update(write_scope=["apps/api/internal"]),
            lambda value: value["parallel_execution"]["leases"]["B"].update(write_scope=["automation"]),
            lambda value: taskctl.get_task(value, "B").update(depends_on=["C"]),
            lambda value: taskctl.get_task(value, "B").update(completion_scope="LIVE_ONLY"),
            lambda value: value["parallel_execution"]["leases"].update(UNKNOWN={"owner": "ghost", "write_scope": ["docs"], "leased_at": "test"}),
        )
        for index, mutation in enumerate(mutations):
            with self.subTest(mutation=index):
                forged = copy.deepcopy(valid)
                mutation(forged)
                with self.assertRaises(SystemExit):
                    taskctl.validate(forged)

    def test_cli_repeated_scopes_and_release_flow(self):
        for arguments in (("parallel-config", "--coordinator", "root", "--limit", "3"),
                          ("lease", "CURRENT", "--coordinator", "root", "--owner", "root", "--write-scope", "apps/api/internal/agentprofile"),
                          ("start", "B", "--parallel", "--coordinator", "root", "--owner", "worker-b", "--write-scope", "docs/research/b.md", "--write-scope", "automation/b.py"),
                          ("done", "B", "--coordinator", "root", "--evidence", "isolated CLI proof"), ("validate",)):
            result = self.cli(*arguments)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        data = taskctl.load(self.queue)
        self.assertEqual(len(data["parallel_execution"]["lease_history"]), 1)
        self.assertEqual(data["closed_pilot_ready"], "NO")

    def test_all_live_task_objects_and_existing_leases_preserved_on_isolated_config(self):
        # Read the official queue once, then operate exclusively on a disposable
        # copy. Another coordinator may legitimately update live evidence.
        original = json.loads(taskctl.QUEUE.read_text(encoding="utf-8"))
        self.replace(original)
        active = [task["id"] for task in original["tasks"] if task["status"] == "IN_PROGRESS"]
        metadata = original.get("parallel_execution", {})
        self.assertLessEqual(len(active), metadata.get("limit", 1))
        existing_leases = copy.deepcopy(metadata.get("leases", {}))
        self.configure()
        for task_id in active:
            if task_id not in existing_leases:
                self.invoke(taskctl.cmd_lease, task_id, "fixture-" + task_id,
                            ["work/parallel-fixture-" + task_id + ".json"], "root")
        for task_id, lease in existing_leases.items():
            self.assertEqual(taskctl.load(self.queue)["parallel_execution"]["leases"][task_id], lease)
        self.assertEqual(taskctl.load(self.queue)["tasks"], original["tasks"])
        self.assertEqual(len(taskctl.load(self.queue)["tasks"]), len(original["tasks"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
