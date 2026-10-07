"""Repository-local AGE/AIR import tests using disposable queue copies only.

These tests verify import and evidence-preservation contracts. They do not prove
that functional AGE/AIR requirements, provider canaries, or release gates pass.
Run with the bundled Python: python -B automation/test_integrate_v5_materials.py
"""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
AUTOMATION = ROOT / "automation"
sys.path.insert(0, str(AUTOMATION))
spec = importlib.util.spec_from_file_location(
    "v5_importer_under_test", AUTOMATION / "integrate_v5_materials.py"
)
IMPORTER = importlib.util.module_from_spec(spec)
spec.loader.exec_module(IMPORTER)


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class V5MaterialImportTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source_plan = read_json(ROOT / "work/v5-queue-increment-plan.json")
        cls.source_base = read_json(ROOT / "work/v5-queue-before-append.json")
        if len(cls.source_base["tasks"]) != 144:
            raise AssertionError("The actual pre-import 144-row checkpoint is required")
        if len(cls.source_plan["proposed_task_rows"]) != 108:
            raise AssertionError("The reviewed 108-row proposal is required")

    def setUp(self):
        self.base = copy.deepcopy(self.source_base)
        self.plan = copy.deepcopy(self.source_plan)

    def candidate(self, base=None, plan=None):
        return IMPORTER.candidate(
            self.base if base is None else base,
            self.plan if plan is None else plan,
        )

    def assert_rejected(self, plan):
        before = copy.deepcopy(self.base)
        with self.assertRaises((AssertionError, SystemExit, ValueError)):
            self.candidate(plan=plan)
        self.assertEqual(self.base, before, "Failed validation must not mutate the input queue")

    def isolated_repository(self, directory, base=None, plan=None):
        """Copy public audit inputs and scripts; all CLI destinations are temporary."""
        isolated = Path(directory)
        (isolated / "automation").mkdir()
        (isolated / "work").mkdir()
        # Task/mapping checkpoints stay historical. CLI copies use their current
        # copied material digests: the canonical UX document legitimately evolves
        # after import. Never change the historical plan or weaken the importer.
        selected_plan = copy.deepcopy(self.plan if plan is None else plan)
        for script in ("taskctl.py", "integrate_v5_materials.py"):
            shutil.copyfile(AUTOMATION / script, isolated / "automation" / script)
        for material in selected_plan["materials"]:
            source = ROOT / material["path"]
            destination = isolated / material["path"]
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, destination)
            material["sha256"] = sha256(destination)
        write_json(isolated / "work/v5-queue-increment-plan.json", selected_plan)
        write_json(isolated / "automation/codex_task_queue.json", self.base if base is None else base)
        return isolated

    def test_isolated_cli_material_digests_match_copies_without_rewriting_plan(self):
        before = copy.deepcopy(self.plan)
        original_bytes = (ROOT / "work/v5-queue-increment-plan.json").read_bytes()
        with tempfile.TemporaryDirectory(prefix="birdtie-v5-import-fixture-") as directory:
            isolated = self.isolated_repository(directory)
            pinned = read_json(isolated / "work/v5-queue-increment-plan.json")
            for material in pinned["materials"]:
                self.assertEqual(material["sha256"], sha256(isolated / material["path"]))
            self.assertEqual(pinned["proposed_task_rows"], before["proposed_task_rows"])
            self.assertEqual(pinned["mapping_rows"], before["mapping_rows"])
        self.assertEqual(self.plan, before)
        self.assertEqual((ROOT / "work/v5-queue-increment-plan.json").read_bytes(), original_bytes)

    def run_cli(self, isolated, *args):
        environment = dict(os.environ, PYTHONDONTWRITEBYTECODE="1", PYTHONIOENCODING="utf-8")
        return subprocess.run(
            [sys.executable, "-B", str(isolated / "automation/integrate_v5_materials.py"), *args],
            cwd=isolated,
            env=environment,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            check=False,
            timeout=30,
        )

    def assert_cli_pass(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_real_proposal_appends_108_unique_rows_without_mutating_inputs(self):
        before_base, before_plan = copy.deepcopy(self.base), copy.deepcopy(self.plan)
        output, additions = self.candidate()
        self.assertEqual(len(additions), 108)
        self.assertEqual(len({t["id"] for t in additions}), 108)
        self.assertEqual(len(output["tasks"]), 252)
        self.assertEqual(output["tasks"][:144], before_base["tasks"])
        self.assertEqual(self.base, before_base)
        self.assertEqual(self.plan, before_plan)
        self.assertIsNot(output["tasks"][0], self.base["tasks"][0])
        IMPORTER.taskctl.validate(output)

    def test_all_old_statuses_evidence_owner_lease_and_unknown_fields_survive(self):
        statuses = ("DONE", "TODO", "PARTIAL", "BLOCKED", "IN_PROGRESS")
        for index, status in enumerate(statuses):
            task = self.base["tasks"][index]
            task["status"] = status
            task["evidence"] = "user-preserved evidence: " + status
            task["owner"] = {"name": "existing-owner", "assigned_by": "human"}
            task["lease"] = {"holder": "existing-worker", "expires_at": "2026-10-03T00:00:00Z"}
            task["user_extra"] = {"nested": [False, 0, "中文记录"]}
            if status == "BLOCKED":
                task["blocked_reason"] = "External condition remains missing"
            if status == "PARTIAL":
                task["partial_reason"] = "Actual partial implementation remains partial"
        self.base["user_metadata"] = {"do_not_overwrite": [1, 2, "original"]}
        expected = copy.deepcopy(self.base)
        output, _ = self.candidate()
        self.assertEqual(output["tasks"][:144], expected["tasks"])
        self.assertEqual(output["user_metadata"], expected["user_metadata"])
        self.assertEqual(self.base, expected)
        self.assertEqual(sum(t["status"] == "IN_PROGRESS" for t in output["tasks"]), 1)

    def test_repeat_import_appends_zero_and_preserves_evolved_v5_rows(self):
        output, _ = self.candidate()
        tasks = {t["id"]: t for t in output["tasks"]}
        tasks["BT-V5-INT-001"].update(status="DONE", evidence="actual phase-zero evidence", owner="user-owner")
        tasks["BT-V5-AGE-001"].update(status="IN_PROGRESS", lease={"holder": "existing-worker"})
        tasks["BT-V5-AGE-002"].update(status="PARTIAL", partial_reason="Actual field evidence outstanding")
        tasks["BT-V5-AGE-003"].update(status="BLOCKED", blocked_reason="Human visibility decision outstanding")
        before = copy.deepcopy(output)
        repeated, additions = self.candidate(base=output)
        self.assertEqual(additions, [])
        self.assertEqual(repeated, before)
        self.assertEqual(output, before)

    def test_source_137_exact_coverage_and_operation_counts(self):
        expected = {f"AGE-{number:03d}" for number in range(1, 82)}
        expected |= {f"BT-V5-AIR-{number:03d}" for number in range(1, 57)}
        mappings = self.plan["mapping_rows"]
        self.assertEqual({m["source_id"] for m in mappings}, expected)
        self.assertEqual(len(mappings), 137)
        self.assertEqual(sum(m["operation"] == "REUSE_EXISTING" for m in mappings), 6)
        self.assertEqual(sum(m["operation"] == "VERIFY_EXISTING" for m in mappings), 3)
        self.assertEqual(sum(m["operation"] == "APPEND_DELTA" for m in mappings), 128)
        self.assertTrue(all(m.get("source_requirement") for m in mappings))

    def test_every_mapping_and_alias_resolves_to_actual_task_ids(self):
        output, _ = self.candidate()
        ids = {t["id"] for t in output["tasks"]}
        fields = ("existing_refs", "proposed_task_ids", "dependency_alias_targets", "proposed_dependencies", "source_completion_requires_all")
        for mapping in self.plan["mapping_rows"]:
            with self.subTest(source=mapping["source_id"]):
                for field in fields:
                    self.assertTrue(set(mapping.get(field, [])) <= ids, field)
                self.assertTrue(set(mapping.get("verification_contract", {}).get("prerequisite_task_ids", [])) <= ids)
        for source, targets in self.plan["source_dependency_aliases"].items():
            with self.subTest(alias=source):
                self.assertTrue(set(targets) <= ids)

    def test_reused_and_verify_sources_do_not_add_parallel_task_ids(self):
        output, _ = self.candidate()
        ids = {t["id"] for t in output["tasks"]}
        for mapping in self.plan["mapping_rows"]:
            if mapping["operation"] in {"REUSE_EXISTING", "VERIFY_EXISTING"}:
                with self.subTest(source=mapping["source_id"]):
                    self.assertEqual(mapping["proposed_task_ids"], [])
                    self.assertNotIn(mapping["normalized_source_id"], ids)
                    self.assertTrue(mapping["dependency_alias_targets"])

    def test_five_live_gates_stay_blocked_and_code_is_not_live_evidence(self):
        output, _ = self.candidate()
        tasks = {t["id"]: t for t in output["tasks"]}
        live = [t for t in output["tasks"] if t["id"].startswith("BT-V5-") and t["id"].endswith("-LIVE")]
        self.assertEqual({t["id"] for t in live}, set(self.plan["external_gates"]["provider_live_rows"]))
        self.assertEqual(len(live), 5)
        for task in live:
            with self.subTest(live=task["id"]):
                self.assertEqual(task["status"], "BLOCKED")
                self.assertEqual(task["completion_scope"], "LIVE_ONLY")
                self.assertTrue(task["blocked_reason"].startswith("BLOCKED_EXTERNAL"))
                self.assertFalse(task["evidence"])
                self.assertTrue(task["external_gate_ids"])
                code = tasks[task["id"].removesuffix("-LIVE")]
                self.assertEqual(code["status"], "TODO")
                self.assertIn("代码/本地", code["title"])
                self.assertTrue(all(" 正向：" not in text for text in code["acceptance"]))
        self.assertEqual(self.plan["external_gates"]["closed_pilot_ready"], "NO")
        self.assertEqual(self.plan["external_gates"]["consumer_beta_ready"], "NO")
        for gate in ("model_egress_enabled", "agent_real_writes_enabled", "vision_enabled", "a2a_enabled"):
            self.assertIs(self.plan["external_gates"][gate], False)

    def test_air042_remains_p2_post_pilot_and_disabled_with_existing_transport(self):
        output, _ = self.candidate()
        task = next(t for t in output["tasks"] if t["id"] == "BT-V5-AIR-042")
        self.assertEqual(task["priority"], "P2")
        self.assertEqual(task["release_stage"], "V4_POST_PILOT")
        self.assertIn("BT-V4-AGA-002", task["depends_on"])
        self.assertIn("a2a feature flag remains OFF", task["activation_gates"])

    def test_source_completion_verify_overlays_wait_for_actual_authority_runtime(self):
        for mapping in self.plan["mapping_rows"]:
            if mapping["operation"] == "VERIFY_EXISTING":
                with self.subTest(source=mapping["source_id"]):
                    self.assertTrue(mapping["verification_contract"]["acceptance"])
                    self.assertEqual(mapping["source_completion_requires_all"], mapping["verification_contract"]["prerequisite_task_ids"])
                    self.assertTrue(any(t.startswith("BT-V5-AGE-") for t in mapping["source_completion_requires_all"]))

    def test_unknown_task_dependency_is_rejected_without_input_mutation(self):
        self.plan["proposed_task_rows"][0]["depends_on"].append("UNKNOWN-DEPENDENCY")
        self.assert_rejected(self.plan)

    def test_new_to_old_dependency_cycle_is_rejected_without_input_mutation(self):
        first = self.plan["proposed_task_rows"][0]
        first["depends_on"].append(first["id"])
        self.assert_rejected(self.plan)

    def test_cross_version_cycle_is_rejected_without_input_mutation(self):
        first_id = self.plan["proposed_task_rows"][0]["id"]
        checkpoint = next(t for t in self.base["tasks"] if t["id"] == "BT-V4-SAF-004")
        checkpoint["depends_on"].append(first_id)
        self.assert_rejected(self.plan)

    def test_duplicate_proposed_ids_are_rejected_instead_of_silently_dropped(self):
        self.plan["proposed_task_rows"][1]["id"] = self.plan["proposed_task_rows"][0]["id"]
        self.assert_rejected(self.plan)

    def test_new_task_cannot_be_imported_done_or_with_claimed_evidence(self):
        for change in ({"status": "DONE", "evidence": "Unverified implementation claim"}, {"evidence": "Previously fabricated evidence"}):
            with self.subTest(change=change):
                plan = copy.deepcopy(self.plan)
                plan["proposed_task_rows"][0].update(change)
                self.assert_rejected(plan)

    def test_live_rows_cannot_be_ready_or_relabelled_as_local(self):
        for change in ({"status": "TODO"}, {"completion_scope": "CODE_AND_LOCAL_VERIFICATION"}):
            with self.subTest(change=change):
                plan = copy.deepcopy(self.plan)
                next(t for t in plan["proposed_task_rows"] if t["id"].endswith("-LIVE")).update(change)
                self.assert_rejected(plan)

    def test_duplicate_or_missing_source_mapping_is_rejected(self):
        plan = copy.deepcopy(self.plan)
        plan["mapping_rows"].pop()
        self.assert_rejected(plan)
        plan = copy.deepcopy(self.plan)
        plan["mapping_rows"][1]["source_id"] = plan["mapping_rows"][0]["source_id"]
        self.assert_rejected(plan)

    def test_unknown_source_cannot_replace_one_of_the_actual_137_requirements(self):
        self.plan["mapping_rows"][0]["source_id"] = "UNKNOWN-SOURCE-001"
        self.assert_rejected(self.plan)

    def test_unknown_mapping_task_references_are_rejected(self):
        for field in ("existing_refs", "proposed_task_ids", "dependency_alias_targets", "proposed_dependencies", "source_completion_requires_all"):
            with self.subTest(field=field):
                plan = copy.deepcopy(self.plan)
                plan["mapping_rows"][0].setdefault(field, []).append("UNKNOWN-MAPPED-TASK")
                self.assert_rejected(plan)

    def test_unknown_source_dependency_alias_target_is_rejected(self):
        self.plan["source_dependency_aliases"]["AGE-001"].append("UNKNOWN-ALIAS-TASK")
        self.assert_rejected(self.plan)

    def test_unknown_verify_overlay_prerequisite_is_rejected(self):
        mapping = next(m for m in self.plan["mapping_rows"] if m["operation"] == "VERIFY_EXISTING")
        mapping["verification_contract"]["prerequisite_task_ids"].append("UNKNOWN-VERIFY-PREREQUISITE")
        self.assert_rejected(self.plan)

    def test_live_gates_cannot_be_turned_on_by_import_proposal(self):
        for key, value in (("closed_pilot_ready", "YES"), ("consumer_beta_ready", "YES"), ("model_egress_enabled", True), ("agent_real_writes_enabled", True), ("vision_enabled", True), ("a2a_enabled", True)):
            with self.subTest(gate=key):
                plan = copy.deepcopy(self.plan)
                plan["external_gates"][key] = value
                self.assert_rejected(plan)

    def test_cli_dry_run_preserves_queue_bytes_and_does_not_write_mapping(self):
        with tempfile.TemporaryDirectory(prefix="birdtie-v5-import-dry-") as directory:
            isolated = self.isolated_repository(directory)
            queue = isolated / "automation/codex_task_queue.json"
            before = queue.read_bytes()
            self.assert_cli_pass(self.run_cli(isolated))
            self.assertEqual(queue.read_bytes(), before)
            self.assertFalse((isolated / "automation/v5_requirement_mapping.json").exists())
            self.assertFalse((isolated / "work/v5-queue-before-append.json").exists())
            report = read_json(isolated / "work/v5-queue-append-dry-run.json")
            self.assertEqual(report["addedRows"], 108)
            self.assertEqual(report["total"], 252)
            self.assertEqual(report["secondAppendNewRows"], 0)
            self.assertEqual(report["beforeSHA256"], report["afterSHA256"])
            self.assertFalse(report["closedPilotReady"])
            self.assertFalse(report["liveEnabled"])

    def test_cli_apply_backup_mapping_old_metadata_and_repeat_zero(self):
        self.base["user_metadata"] = {"language": "中文优先", "extra": ["preserve"]}
        self.base["tasks"][0]["owner"] = "human-existing-owner"
        self.base["tasks"][0]["lease"] = {"holder": "existing-worker", "opaque": 123}
        with tempfile.TemporaryDirectory(prefix="birdtie-v5-import-apply-") as directory:
            isolated = self.isolated_repository(directory)
            queue = isolated / "automation/codex_task_queue.json"
            before = queue.read_bytes()
            self.assert_cli_pass(self.run_cli(isolated, "--apply"))
            self.assertEqual((isolated / "work/v5-queue-before-append.json").read_bytes(), before)
            saved = read_json(queue)
            self.assertEqual(saved["tasks"][:144], self.base["tasks"])
            self.assertEqual(saved["user_metadata"], self.base["user_metadata"])
            self.assertEqual(len(saved["v5_material_integration"]["added_task_ids"]), 108)
            self.assertTrue(saved["v5_material_integration"]["closed_pilot_gate_unchanged"])
            mapping_path = isolated / "automation/v5_requirement_mapping.json"
            mapping = read_json(mapping_path)
            self.assertEqual(mapping["mapping_rows"], self.plan["mapping_rows"])
            self.assertEqual(mapping["source_requirements"], 137)
            self.assertTrue(mapping["source_completion_not_inferred_from_task_done_alone"])
            queue_after, map_after = queue.read_bytes(), mapping_path.read_bytes()
            self.assert_cli_pass(self.run_cli(isolated, "--apply"))
            self.assertEqual(queue.read_bytes(), queue_after)
            self.assertEqual(mapping_path.read_bytes(), map_after)
            self.assertEqual(read_json(isolated / "work/v5-queue-append-result.json")["addedRows"], 0)

    def test_cli_material_digest_mismatch_rejects_before_any_queue_write(self):
        with tempfile.TemporaryDirectory(prefix="birdtie-v5-import-source-") as directory:
            isolated = self.isolated_repository(directory)
            queue = isolated / "automation/codex_task_queue.json"
            before = queue.read_bytes()
            material = isolated / self.plan["materials"][0]["path"]
            material.write_bytes(material.read_bytes() + b"\nchanged\n")
            result = self.run_cli(isolated, "--apply")
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(queue.read_bytes(), before)
            self.assertFalse((isolated / "automation/v5_requirement_mapping.json").exists())
            self.assertFalse((isolated / "work/v5-queue-before-append.json").exists())

    def test_cli_checkpoint_missing_status_or_actual_evidence_rejects(self):
        for change in ({"status": "TODO", "evidence": ""}, {"status": "DONE", "evidence": "Unrelated report alone"}):
            with self.subTest(change=change), tempfile.TemporaryDirectory(prefix="birdtie-v5-import-gate-") as directory:
                base = copy.deepcopy(self.base)
                next(t for t in base["tasks"] if t["id"] == "BT-V4-SAF-004").update(change)
                isolated = self.isolated_repository(directory, base=base)
                queue = isolated / "automation/codex_task_queue.json"
                before = queue.read_bytes()
                self.assertNotEqual(self.run_cli(isolated, "--apply").returncode, 0)
                self.assertEqual(queue.read_bytes(), before)
                self.assertFalse((isolated / "automation/v5_requirement_mapping.json").exists())

    def test_cli_must_not_overwrite_a_preexisting_backup(self):
        with tempfile.TemporaryDirectory(prefix="birdtie-v5-import-backup-") as directory:
            isolated = self.isolated_repository(directory)
            queue = isolated / "automation/codex_task_queue.json"
            backup = isolated / "work/v5-queue-before-append.json"
            backup.write_bytes(b"user-existing-backup\n")
            before = queue.read_bytes()
            self.assertNotEqual(self.run_cli(isolated, "--apply").returncode, 0)
            self.assertEqual(queue.read_bytes(), before)
            self.assertEqual(backup.read_bytes(), b"user-existing-backup\n")
            self.assertFalse((isolated / "automation/v5_requirement_mapping.json").exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
