"""Local preview lifecycle tests. All external process/device operations mocked."""
import json
from pathlib import Path
import subprocess
import tempfile
import types
import unittest
from unittest.mock import Mock, patch

import local_preview_runtime as runtime


class FakeProcess:
    def __init__(self, pid=71):
        self.pid = pid
        self.code = None
        self.terminated = False
        self.killed = False

    def poll(self):
        return self.code

    def terminate(self):
        self.terminated = True
        self.code = 0

    def kill(self):
        self.killed = True
        self.code = 0

    def wait(self, timeout):
        return self.code


class LifecycleTests(unittest.TestCase):
    def setUp(self):
        self.config = types.SimpleNamespace(exe=Path("known-api.exe"), port=18109,
                device_port=18091, device="explicit-phone", adb=Path("adb.exe"),
                validate_files=Mock())
        self.platform = Mock()
        self.platform.listener.return_value = None
        self.platform.ready.return_value = True
        self.platform.tunnel.return_value = "ready"
        self.child = FakeProcess()
        self.platform.launch_api.return_value = self.child
        self.now = 10
        self.supervisor = runtime.Supervisor(self.config, self.platform, clock=lambda: self.now)

    def listener(self, pid=71, exe=None):
        self.platform.listener.return_value = {"pid": pid, "exe": exe or str(self.config.exe)}

    def test_missing_api_starts_only_configured_process(self):
        self.assertEqual(self.supervisor.tick()["api"], "starting")
        self.platform.launch_api.assert_called_once_with(self.config)
        self.listener()
        health = self.supervisor.tick()
        self.assertEqual((health["api"], health["ownership"], health["tunnel"]), ("ready", "owned", "ready"))

    def test_exited_owned_api_recovers_after_backoff(self):
        self.supervisor.tick()
        self.child.code = 1
        self.assertEqual(self.supervisor.tick()["api"], "retry_wait")
        self.now += 5
        new_child = FakeProcess(72)
        self.platform.launch_api.return_value = new_child
        self.assertEqual(self.supervisor.tick()["apiPid"], 72)
        self.assertEqual(self.platform.launch_api.call_count, 2)

    def test_existing_exact_api_is_adopted_and_never_killed(self):
        self.listener(pid=98)
        self.assertEqual(self.supervisor.tick()["ownership"], "adopted")
        self.supervisor.stop_owned_api()
        self.supervisor.stop_owned_api()
        self.platform.launch_api.assert_not_called()
        self.assertIsNone(self.supervisor.child)
        self.assertFalse(self.child.terminated)

    def test_unknown_listener_never_replaced_or_tunneled(self):
        self.listener(pid=98, exe="someone-elses-api.exe")
        self.assertEqual(self.supervisor.tick()["api"], "listener_conflict")
        self.platform.launch_api.assert_not_called()
        self.platform.tunnel.assert_not_called()
        self.supervisor.stop_owned_api()
        self.assertFalse(self.child.terminated)

    def test_port_race_between_inspection_and_launch_is_safe(self):
        self.platform.listener.side_effect = [None, {"pid": 99, "exe": "unknown.exe"}]
        self.assertEqual(self.supervisor.tick()["api"], "retry_wait")
        self.platform.launch_api.assert_not_called()

    def test_stop_owned_process_repeats_safely(self):
        self.supervisor.tick()
        self.listener(pid=99, exe="unknown.exe")
        self.supervisor.tick()
        self.supervisor.stop_owned_api()
        self.supervisor.stop_owned_api()
        self.assertTrue(self.child.terminated)

    def test_hung_owned_start_restarts_but_adopted_unready_does_not(self):
        self.supervisor.tick()
        self.now += 30
        self.assertEqual(self.supervisor.tick()["api"], "restarting")
        self.assertTrue(self.child.terminated)
        self.listener(pid=98)
        self.platform.ready.return_value = False
        for _ in range(10):
            self.assertEqual(self.supervisor.tick()["ownership"], "adopted")
        self.assertEqual(self.platform.launch_api.call_count, 1)

    def test_owned_ready_failures_restart_own_handle(self):
        self.supervisor.tick()
        self.listener()
        self.platform.ready.return_value = False
        for _ in range(5):
            self.assertEqual(self.supervisor.tick()["api"], "unavailable")
        self.assertEqual(self.supervisor.tick()["api"], "restarting")
        self.assertTrue(self.child.terminated)

    def test_new_owned_process_resets_failures_from_unready_adopted_api(self):
        self.listener(pid=98)
        self.platform.ready.return_value = False
        for _ in range(8):
            self.assertEqual(self.supervisor.tick()["ownership"], "adopted")
        self.platform.listener.return_value = None
        self.assertEqual(self.supervisor.tick()["api"], "starting")
        self.listener()
        health = self.supervisor.tick()
        self.assertEqual((health["api"], health["ownership"]), ("unavailable", "owned"))
        self.assertEqual(self.supervisor.failures, 1)
        self.assertFalse(self.child.terminated)

    @patch.object(runtime, "api_environment", return_value={"runtime-key": "mock-secret"})
    @patch.object(runtime, "durable_popen")
    @patch.object(runtime.subprocess, "Popen")
    def test_owned_api_uses_durable_parent_without_requesting_second_breakaway(self, popen, durable, env):
        runtime.Platform().launch_api(self.config)
        durable.assert_not_called()
        self.assertEqual(popen.call_args.args[0], [str(self.config.exe)])
        flags = popen.call_args.kwargs["creationflags"]
        self.assertEqual(flags & getattr(subprocess, "CREATE_BREAKAWAY_FROM_JOB", 0), 0)
        self.assertEqual(popen.call_args.kwargs["env"], {"runtime-key": "mock-secret"})
        self.assertEqual(popen.call_args.kwargs["stdout"], subprocess.DEVNULL)
        self.assertEqual(popen.call_args.kwargs["stderr"], subprocess.DEVNULL)

    @patch.object(runtime, "hidden_run")
    def test_device_disconnect_and_reconnect_restores_explicit_reverse(self, execute):
        result = lambda out="", code=0: types.SimpleNamespace(stdout=out, returncode=code)
        execute.side_effect = [result("offline", 1), result("device"), result(""), result("")]
        platform = runtime.Platform()
        self.assertEqual(platform.tunnel(self.config), "device_offline")
        self.assertEqual(platform.tunnel(self.config), "ready")
        commands = [call.args[0] for call in execute.call_args_list]
        self.assertTrue(all(command[:3] == ["adb.exe", "-s", "explicit-phone"] for command in commands))
        self.assertEqual(commands[-1][3:], ["reverse", "--no-rebind", "tcp:18091", "tcp:18109"])

    @patch.object(runtime, "hidden_run")
    def test_conflicting_device_mapping_is_not_rebound(self, execute):
        execute.side_effect = [types.SimpleNamespace(stdout="device", returncode=0),
                              types.SimpleNamespace(stdout="phone tcp:18091 tcp:9999\n", returncode=0)]
        self.assertEqual(runtime.Platform().tunnel(self.config), "tunnel_conflict")
        self.assertEqual(execute.call_count, 2)

    @patch.object(runtime, "hidden_run")
    def test_existing_correct_tunnel_is_idempotent(self, execute):
        execute.side_effect = [types.SimpleNamespace(stdout="device", returncode=0),
                              types.SimpleNamespace(stdout="phone tcp:18091 tcp:18109\n", returncode=0)]
        self.assertEqual(runtime.Platform().tunnel(self.config), "ready")
        self.assertEqual(execute.call_count, 2)


class ControlTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.config = types.SimpleNamespace(state_dir=Path(self.temp.name),
                path=Path(self.temp.name) / "preview-config.json", fingerprint="known-config")
        self.identity = {"pid": 7, "exe": "python.exe", "created": "known-created-time"}
        self.state = {"supervisor": "running", "supervisorPid": 7,
                      "supervisorIdentity": self.identity, "token": "local-control-token"}
        runtime.write_json(self.config.state_dir / "status.json", self.state)

    def test_diagnostics_include_only_allowlisted_fixed_runtime_error_code(self):
        self.assertEqual(runtime.safe_error_details(RuntimeError("PREVIEW_RUNTIME_CREDENTIAL_MISSING")),
                         {"errorCategory": "RuntimeError", "errorCode": "PREVIEW_RUNTIME_CREDENTIAL_MISSING"})

    def test_unknown_exception_text_never_enters_diagnostics(self):
        for error in (RuntimeError("PREVIEW_mock-secret-value"), ValueError("mock-secret-value"),
                      OSError("mock-secret-value")):
            details = runtime.safe_error_details(error)
            self.assertNotIn("errorCode", details)
            self.assertNotIn("mock-secret-value", json.dumps(details))

    @patch.object(runtime, "process_identity")
    def test_status_hides_control_token_and_pid_reuse_is_stopped(self, identity):
        identity.return_value = self.identity
        self.assertEqual(runtime.status(self.config)["supervisor"], "running")
        self.assertNotIn("token", runtime.status(self.config))
        identity.return_value = {**self.identity, "created": "different-time"}
        self.assertEqual(runtime.status(self.config)["supervisor"], "stopped")

    @patch.object(runtime, "process_identity")
    def test_stop_stale_owner_never_writes_control_request(self, identity):
        identity.return_value = {**self.identity, "created": "different-time"}
        self.assertEqual(runtime.stop(self.config)["supervisor"], "stopped")
        self.assertFalse((self.config.state_dir / "stop.json").exists())

    @patch.object(runtime, "status", return_value={"supervisor": "stopped"})
    @patch.object(runtime, "process_identity")
    def test_stop_uses_token_then_repeats_without_process_kills(self, identity, status):
        identity.return_value = self.identity
        self.assertEqual(runtime.stop(self.config)["supervisor"], "stopped")
        self.assertEqual(runtime.read_json(self.config.state_dir / "stop.json"), {"token": "local-control-token"})
        self.state["supervisor"] = "stopped"
        runtime.write_json(self.config.state_dir / "status.json", self.state)
        self.assertEqual(runtime.stop(self.config)["supervisor"], "stopped")

    @patch.object(runtime, "status")
    @patch.object(runtime, "durable_popen")
    def test_duplicate_start_does_not_launch(self, popen, status):
        status.return_value = {"supervisor": "running", "configuration": str(self.config.path),
                               "configurationFingerprint": self.config.fingerprint}
        self.assertEqual(runtime.start(self.config)["supervisor"], "running")
        popen.assert_not_called()

    @patch.object(runtime, "status")
    @patch.object(runtime, "durable_popen")
    def test_running_configuration_conflict_never_spawns_or_overrides(self, popen, status):
        conflicts = [
            {"configuration": str(self.config.path.with_name("other-config.json")),
             "configurationFingerprint": self.config.fingerprint},
            {"configuration": str(self.config.path), "configurationFingerprint": "different-api-or-device"},
        ]
        for conflict in conflicts:
            with self.subTest(conflict=conflict):
                status.return_value = {"supervisor": "running", **conflict}
                with self.assertRaisesRegex(RuntimeError, "PREVIEW_CONFIGURATION_CONFLICT"):
                    runtime.start(self.config)
        popen.assert_not_called()

    @patch.object(runtime, "status")
    @patch.object(runtime, "durable_popen")
    def test_same_config_file_changed_api_or_device_reports_conflict(self, popen, status):
        raw = {"schemaVersion": "birdtie-local-preview-v1", "apiExe": "known.exe",
               "apiReceipt": "receipt.json", "trialConfig": "trial.json", "liveConfig": "live.json",
               "adbPath": "adb.exe", "stateDirectory": str(self.config.state_dir),
               "apiAddress": "127.0.0.1:18109", "deviceId": "phone-one", "devicePort": 18091,
               "databaseContainer": "known-db", "databasePort": 55432, "apiSha256": "known-hash"}
        runtime.write_json(self.config.path, raw)
        original = runtime.Config(self.config.path, validate=False)
        status.return_value = {"supervisor": "running", "configuration": str(original.path),
                               "configurationFingerprint": original.fingerprint}
        for key, changed in (("apiExe", "other.exe"), ("apiAddress", "127.0.0.1:19000"),
                             ("deviceId", "phone-two"), ("devicePort", 19001)):
            with self.subTest(field=key):
                runtime.write_json(self.config.path, {**raw, key: changed})
                config = runtime.Config(self.config.path, validate=False)
                with self.assertRaisesRegex(RuntimeError, "PREVIEW_CONFIGURATION_CONFLICT"):
                    runtime.start(config)
        popen.assert_not_called()

    @patch.object(runtime, "durable_popen")
    @patch.object(runtime, "status")
    def test_new_start_is_hidden_detached_without_secret_arguments(self, status, popen):
        self.config.path = Path(self.temp.name) / "preview-config.json"
        status.side_effect = [{"supervisor": "stopped"}, {"supervisor": "running",
                "configuration": str(self.config.path), "configurationFingerprint": self.config.fingerprint}]
        with patch.dict(runtime.os.environ, {"BIRDTIE_TENCENT_TOKENHUB_API_KEY": "mock-secret"}):
            self.assertEqual(runtime.start(self.config)["supervisor"], "running")
        command = popen.call_args.args[0]
        self.assertEqual(command[2:], ["run", "--config", str(self.config.path)])
        self.assertNotIn("mock-secret", str(popen.call_args))
        self.assertEqual(popen.call_args.kwargs["stdout"], subprocess.DEVNULL)
        self.assertEqual(popen.call_args.kwargs["stderr"], subprocess.DEVNULL)

    @patch.object(runtime, "durable_popen")
    @patch.object(runtime, "status")
    def test_configuration_race_after_spawn_is_reported_as_conflict(self, status, popen):
        status.side_effect = [{"supervisor": "stopped"}, {"supervisor": "running",
                "configuration": str(self.config.path), "configurationFingerprint": "other-config"}]
        with patch.dict(runtime.os.environ, {"BIRDTIE_TENCENT_TOKENHUB_API_KEY": "mock-secret"}):
            with self.assertRaisesRegex(RuntimeError, "PREVIEW_CONFIGURATION_CONFLICT"):
                runtime.start(self.config)

    @patch.object(runtime, "hidden_run")
    def test_environment_preserves_boundary_and_contains_secret_only_in_memory(self, inspect):
        config = types.SimpleNamespace(validate_files=Mock(), container="known-db", database_port=55432,
                database="existing-db", address="127.0.0.1:18109", live=Path("live.local.json"))
        inspect.return_value = types.SimpleNamespace(returncode=0, stdout=json.dumps([{
            "State": {"Running": True}, "Config": {"Env": ["POSTGRES_USER=user", "POSTGRES_PASSWORD=p@ss"]}}]))
        with patch.dict(runtime.os.environ, {"BIRDTIE_TENCENT_TOKENHUB_API_KEY": "mock-secret",
                "BIRDTIE_DISPOSABLE_DB": "1", "BIRDTIE_MODEL_DEBUG": "1", "BIRDTIE_OIDC_CLIENT_SECRET": "old"}):
            env = runtime.api_environment(config)
        self.assertEqual(env["BIRDTIE_TENCENT_TOKENHUB_API_KEY"], "mock-secret")
        self.assertNotIn("BIRDTIE_DISPOSABLE_DB", env)
        self.assertNotIn("BIRDTIE_MODEL_DEBUG", env)
        self.assertNotIn("BIRDTIE_OIDC_CLIENT_SECRET", env)
        self.assertIn("p%40ss@127.0.0.1:55432/existing-db", env["BIRDTIE_DATABASE_URL"])
        self.assertEqual(json.loads(env["BIRDTIE_AGENT_FEATURE_FLAGS"]), runtime.FLAGS)
        inspect.assert_called_once_with(["docker", "inspect", "known-db"])

    @unittest.skipUnless(runtime.os.name == "nt", "Windows byte lock")
    def test_singleton_lock_excludes_duplicate_and_releases_for_restart(self):
        first = runtime.RuntimeLock(self.config.state_dir)
        second = runtime.RuntimeLock(self.config.state_dir)
        try:
            self.assertTrue(first.acquire())
            self.assertFalse(second.acquire())
            first.close()
            self.assertTrue(second.acquire())
        finally:
            first.close()
            second.close()

    @patch.object(runtime, "status")
    @patch.object(runtime, "durable_popen")
    def test_start_without_credential_does_not_create_supervisor(self, popen, status):
        status.return_value = {"supervisor": "stopped"}
        with patch.dict(runtime.os.environ, {}, clear=True):
            with self.assertRaisesRegex(RuntimeError, "PREVIEW_RUNTIME_CREDENTIAL_MISSING"):
                runtime.start(self.config)
        popen.assert_not_called()


if __name__ == "__main__":
    unittest.main()
