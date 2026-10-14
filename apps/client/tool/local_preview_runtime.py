"""Keep an explicitly configured, existing local preview API and ADB tunnel alive.

No builds, migrations, provider requests, system startup registration or database
creation. Credentials remain in the inherited environment and process memory.
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import time
from urllib.parse import quote
from urllib.request import urlopen


ROOT = Path(__file__).resolve().parents[3]
DEFAULT_CONFIG = ROOT / "apps/client/.env.preview.local.json"
SAFE_ERROR_CODES = frozenset({
    "PREVIEW_CONFIG_VERSION_INVALID", "PREVIEW_LOOPBACK_CONFIG_INVALID",
    "PREVIEW_EXPLICIT_DEVICE_REQUIRED", "PREVIEW_DATABASE_CONTAINER_INVALID",
    "PREVIEW_EXISTING_RUNTIME_MISMATCH", "PREVIEW_RUNTIME_FILE_MISSING",
    "PREVIEW_PROCESS_INSPECTION_FAILED", "PREVIEW_DURABLE_PROCESS_UNAVAILABLE",
    "PREVIEW_RUNTIME_CREDENTIAL_MISSING", "PREVIEW_EXISTING_DATABASE_UNAVAILABLE",
    "PREVIEW_EXISTING_DATABASE_CONFIG_INVALID", "PREVIEW_LISTENER_OWNER_UNKNOWN",
    "PREVIEW_SUPERVISOR_IDENTITY_UNAVAILABLE", "PREVIEW_CONFIGURATION_CONFLICT",
    "PREVIEW_SUPERVISOR_START_FAILED", "PREVIEW_SUPERVISOR_START_TIMEOUT",
})
FLAGS = {
    "schemaVersion": "agent-feature-flags-v1",
    "flags": {"agent_enrichment": True, "agent_memory": False,
              "agent_attention_policy": False, "agent_social_policy": False,
              "life_map": False},
    "pilot": {"memory": "basic", "inference": "conservative",
              "autonomousAction": False, "sensitiveInference": False},
}


def read_json(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def safe_error_details(error):
    result = {"errorCategory": type(error).__name__}
    if isinstance(error, RuntimeError) and str(error) in SAFE_ERROR_CODES:
        result["errorCode"] = str(error)
    return result


def write_json(path, value):
    path = Path(path)
    temporary = path.with_name(path.name + ".tmp." + secrets.token_hex(6))
    temporary.write_text(json.dumps(value, indent=2), encoding="utf-8")
    os.replace(temporary, path)


def same_path(left, right):
    return str(Path(left).resolve()).casefold() == str(Path(right).resolve()).casefold()


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


class Config:
    def __init__(self, path=DEFAULT_CONFIG, validate=True):
        self.path = Path(path).resolve()
        raw = read_json(self.path)
        if raw.get("schemaVersion") != "birdtie-local-preview-v1":
            raise RuntimeError("PREVIEW_CONFIG_VERSION_INVALID")
        # Canonical JSON detects changed API/device settings even when the same
        # configuration filename and state directory are reused.
        self.fingerprint = hashlib.sha256(json.dumps(raw, sort_keys=True,
                separators=(",", ":")).encode("utf-8")).hexdigest()

        def file_path(key, default=None):
            candidate = Path(raw.get(key, default))
            return candidate.resolve() if candidate.is_absolute() else (ROOT / candidate).resolve()

        self.exe = file_path("apiExe")
        self.receipt = file_path("apiReceipt")
        self.trial = file_path("trialConfig")
        self.live = file_path("liveConfig")
        self.adb = file_path("adbPath")
        self.state_dir = file_path("stateDirectory", "work/local-preview-runtime")
        self.address = raw["apiAddress"]
        host, port = self.address.rsplit(":", 1)
        self.port = int(port)
        self.device_port = int(raw["devicePort"])
        self.device = raw["deviceId"]
        self.container = raw["databaseContainer"]
        self.database_port = int(raw["databasePort"])
        self.sha = raw["apiSha256"]
        if host != "127.0.0.1" or any(not 1 <= p <= 65535 for p in
                (self.port, self.device_port, self.database_port)):
            raise RuntimeError("PREVIEW_LOOPBACK_CONFIG_INVALID")
        if not isinstance(self.device, str) or not self.device.strip() or self.device.startswith("-"):
            raise RuntimeError("PREVIEW_EXPLICIT_DEVICE_REQUIRED")
        if not isinstance(self.container, str) or not self.container or self.container.startswith("-"):
            raise RuntimeError("PREVIEW_DATABASE_CONTAINER_INVALID")
        if validate:
            self.validate_files()

    def validate_files(self):
        receipt = read_json(self.receipt)
        trial = read_json(self.trial)
        if (not same_path(receipt["exe"], self.exe) or receipt["address"] != self.address
                or receipt["apiSHA256"] != self.sha or digest(self.exe) != self.sha
                or trial["database"] != receipt["database"]):
            raise RuntimeError("PREVIEW_EXISTING_RUNTIME_MISMATCH")
        if not self.live.is_file() or not self.adb.is_file():
            raise RuntimeError("PREVIEW_RUNTIME_FILE_MISSING")
        self.database = trial["database"]


def hidden_run(argv, timeout=10):
    # Lists are passed directly to CreateProcess. Never interpolate shell commands.
    return subprocess.run(argv, capture_output=True, text=True, timeout=timeout,
                          creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))


def windows_json(script):
    powershell = Path(os.environ.get("SystemRoot", "C:/Windows")) / "System32/WindowsPowerShell/v1.0/powershell.exe"
    encoded = base64.b64encode(script.encode("utf-16-le")).decode("ascii")
    result = hidden_run([str(powershell), "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded])
    if result.returncode:
        raise RuntimeError("PREVIEW_PROCESS_INSPECTION_FAILED")
    return json.loads(result.stdout.strip() or "null")


def process_identity(pid):
    value = windows_json(
        f"$p=Get-CimInstance Win32_Process -Filter 'ProcessId={int(pid)}'; "
        "if($p){[pscustomobject]@{pid=[int]$p.ProcessId;exe=$p.ExecutablePath;"
        "created=$p.CreationDate.ToUniversalTime().ToString('o')}|ConvertTo-Json -Compress}")
    return value if value and value.get("exe") else None


def durable_popen(argv, **kwargs):
    if os.name != "nt":
        return subprocess.Popen(argv, start_new_session=True, **kwargs)
    flags = subprocess.CREATE_NO_WINDOW | subprocess.CREATE_NEW_PROCESS_GROUP
    try:
        return subprocess.Popen(argv, creationflags=flags | subprocess.CREATE_BREAKAWAY_FROM_JOB, **kwargs)
    except OSError as error:
        # Breakaway may be denied even outside a Job. Fall back only when there is
        # no enclosing Job; otherwise an apparently detached process would die
        # when the short-lived tool command is cleaned up.
        import ctypes
        in_job = ctypes.c_int()
        kernel = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel.GetCurrentProcess.restype = ctypes.c_void_p
        kernel.IsProcessInJob.argtypes = [ctypes.c_void_p, ctypes.c_void_p, ctypes.POINTER(ctypes.c_int)]
        if (error.winerror not in (5, 87) or not kernel.IsProcessInJob(
                kernel.GetCurrentProcess(), None, ctypes.byref(in_job)) or in_job.value):
            raise RuntimeError("PREVIEW_DURABLE_PROCESS_UNAVAILABLE") from None
        return subprocess.Popen(argv, creationflags=flags, **kwargs)


def api_environment(config):
    if not os.environ.get("BIRDTIE_TENCENT_TOKENHUB_API_KEY"):
        raise RuntimeError("PREVIEW_RUNTIME_CREDENTIAL_MISSING")
    config.validate_files()
    result = hidden_run(["docker", "inspect", config.container])
    if result.returncode:
        raise RuntimeError("PREVIEW_EXISTING_DATABASE_UNAVAILABLE")
    try:
        container = json.loads(result.stdout)[0]
        if not container["State"]["Running"]:
            raise ValueError()
        variables = dict(line.split("=", 1) for line in container["Config"]["Env"] if "=" in line)
        user, password = variables["POSTGRES_USER"], variables["POSTGRES_PASSWORD"]
    except (KeyError, IndexError, TypeError, ValueError):
        raise RuntimeError("PREVIEW_EXISTING_DATABASE_CONFIG_INVALID") from None
    env = os.environ.copy()
    for key in list(env):
        if key.startswith(("BIRDTIE_OIDC_", "BIRDTIE_FEATURE_", "BIRDTIE_MODEL_")):
            env.pop(key)
    env.pop("BIRDTIE_DISPOSABLE_DB", None)
    env.update({
        "BIRDTIE_DATABASE_URL": "postgresql://" + quote(user, safe="") + ":" + quote(password, safe="")
        + "@127.0.0.1:" + str(config.database_port) + "/" + quote(config.database, safe="") + "?sslmode=disable",
        "BIRDTIE_API_ADDR": config.address,
        "BIRDTIE_ALLOWED_ORIGINS": "http://localhost:7357",
        "BIRDTIE_DEV_PHONE_AUTH": "true",
        "BIRDTIE_OPEN_PLACES_ENABLED": "1",
        "BIRDTIE_NOW_LIVE_CONFIG": str(config.live),
        "BIRDTIE_AGENT_FEATURE_FLAGS": json.dumps(FLAGS),
    })
    return env


class Platform:
    def listener(self, config):
        values = windows_json(
            f"$c=@(Get-NetTCPConnection -State Listen -LocalPort {config.port} -ErrorAction SilentlyContinue); "
            "@($c|Select-Object -ExpandProperty OwningProcess -Unique)|ConvertTo-Json -Compress")
        pids = values if isinstance(values, list) else [values] if values is not None else []
        with socket.socket() as sock:
            sock.settimeout(.5)
            listening = sock.connect_ex(("127.0.0.1", config.port)) == 0
        if not pids:
            if listening:
                raise RuntimeError("PREVIEW_LISTENER_OWNER_UNKNOWN")
            return None
        if len(pids) != 1:
            return {"pid": None, "exe": None}
        return process_identity(pids[0]) or {"pid": int(pids[0]), "exe": None}

    def ready(self, config):
        try:
            with urlopen("http://" + config.address + "/readyz", timeout=1) as response:
                return response.status == 200 and json.load(response).get("status") == "ready"
        except Exception:
            return False

    def launch_api(self, config):
        # The supervisor has already escaped the short-lived command Job. Its
        # owned children must stay with that durable parent; requesting another
        # breakaway can be denied by an enclosing, persistent desktop Job.
        flags = (getattr(subprocess, "CREATE_NO_WINDOW", 0)
                 | getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0))
        return subprocess.Popen([str(config.exe)], cwd=ROOT / "apps/api", env=api_environment(config),
                                stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                                stderr=subprocess.DEVNULL, creationflags=flags)

    def tunnel(self, config):
        prefix = [str(config.adb), "-s", config.device]
        state = hidden_run(prefix + ["get-state"], timeout=5)
        if state.returncode or state.stdout.strip() != "device":
            return "device_offline"
        listing = hidden_run(prefix + ["reverse", "--list"], timeout=5)
        if listing.returncode:
            return "tunnel_unavailable"
        local, remote = f"tcp:{config.device_port}", f"tcp:{config.port}"
        for line in listing.stdout.splitlines():
            parts = line.split()
            if len(parts) == 3 and parts[1] == local:
                return "ready" if parts[2] == remote else "tunnel_conflict"
        # --no-rebind protects a concurrent tunnel owned by another session.
        reverse = hidden_run(prefix + ["reverse", "--no-rebind", local, remote], timeout=5)
        return "ready" if reverse.returncode == 0 else "tunnel_unavailable"


class Supervisor:
    def __init__(self, config, platform=None, clock=time.monotonic):
        self.config = config
        self.platform = platform or Platform()
        self.clock = clock
        self.child = None
        self.next_launch = 0
        self.failures = 0
        self.launches = 0
        self.launched_at = 0

    def tick(self):
        if self.child is not None and self.child.poll() is not None:
            self.child = None
            self.failures = 0
            self.next_launch = self.clock() + 5
        listener = self.platform.listener(self.config)
        if listener:
            if not listener.get("exe") or not same_path(listener["exe"], self.config.exe):
                return {"api": "listener_conflict", "ownership": "foreign", "tunnel": "waiting_for_api"}
            self.config.validate_files()
            owned = self.child is not None and listener["pid"] == self.child.pid
            ready = self.platform.ready(self.config)
            self.failures = 0 if ready else self.failures + 1
            if owned and self.failures >= 6:
                self.stop_owned_api()
                self.next_launch = self.clock() + 5
                return {"api": "restarting", "ownership": "none", "tunnel": "waiting_for_api"}
            return {"api": "ready" if ready else "unavailable", "ownership": "owned" if owned else "adopted",
                    "apiPid": listener["pid"], "tunnel": self.platform.tunnel(self.config) if ready else "waiting_for_api"}
        if self.child is not None:
            if self.clock() - self.launched_at >= 30:
                self.stop_owned_api()
                self.next_launch = self.clock() + 5
                return {"api": "restarting", "ownership": "none", "tunnel": "waiting_for_api"}
            return {"api": "starting", "ownership": "owned", "apiPid": self.child.pid, "tunnel": "waiting_for_api"}
        if self.clock() < self.next_launch:
            return {"api": "retry_wait", "ownership": "none", "tunnel": "waiting_for_api"}
        # Recheck immediately before launch to avoid racing another listener.
        if self.platform.listener(self.config) is not None:
            return {"api": "retry_wait", "ownership": "none", "tunnel": "waiting_for_api"}
        self.child = self.platform.launch_api(self.config)
        self.failures = 0
        self.launched_at = self.clock()
        self.launches += 1
        return {"api": "starting", "ownership": "owned", "apiPid": self.child.pid, "tunnel": "waiting_for_api"}

    def stop_owned_api(self):
        child, self.child = self.child, None
        if child is not None and child.poll() is None:
            # Popen retains a process handle on Windows, so PID reuse cannot
            # turn this into a kill of an unrelated listener or adopted API.
            child.terminate()
            try:
                child.wait(timeout=5)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)


class RuntimeLock:
    def __init__(self, directory):
        directory.mkdir(parents=True, exist_ok=True)
        self.file = (directory / "supervisor.lock").open("a+b")
        self.file.seek(0)
        if not self.file.read(1):
            self.file.write(b"0")
            self.file.flush()
        self.file.seek(0)

    def acquire(self):
        import msvcrt
        try:
            msvcrt.locking(self.file.fileno(), msvcrt.LK_NBLCK, 1)
            return True
        except OSError:
            return False

    def close(self):
        self.file.close()


def state_matches_process(state):
    if not state or state.get("supervisor") != "running":
        return False
    identity = process_identity(state["supervisorPid"])
    return identity is not None and identity == state.get("supervisorIdentity")


def status(config):
    path = config.state_dir / "status.json"
    state = read_json(path) if path.is_file() else {"supervisor": "stopped"}
    if state.get("supervisor") == "running" and not state_matches_process(state):
        state.update(supervisor="stopped", api="unknown", tunnel="unknown")
    # The control token is local IPC material, never a console or log value.
    return {key: value for key, value in state.items() if key != "token"}


def run_supervisor(config):
    lock = RuntimeLock(config.state_dir)
    if not lock.acquire():
        lock.close()
        return 0
    supervisor = Supervisor(config)
    identity = process_identity(os.getpid())
    if identity is None:
        lock.close()
        raise RuntimeError("PREVIEW_SUPERVISOR_IDENTITY_UNAVAILABLE")
    state = {"supervisor": "running", "supervisorPid": os.getpid(), "supervisorIdentity": identity,
             "token": secrets.token_hex(24), "apiAddress": config.address, "devicePort": config.device_port,
             "deviceId": config.device, "configuration": str(config.path),
             "configurationFingerprint": config.fingerprint}
    status_path = config.state_dir / "status.json"
    stop_path = config.state_dir / "stop.json"
    previous = None
    try:
        while True:
            if stop_path.is_file() and read_json(stop_path).get("token") == state["token"]:
                stop_path.unlink()
                break
            try:
                health = supervisor.tick()
            except Exception as error:
                # Exception strings or child output can contain credentials.
                # Publish an error category only; retry without killing adopted services.
                health = {"api": "unavailable", "ownership": "owned" if supervisor.child else "none",
                          "tunnel": "unknown", **safe_error_details(error)}
            for key in ("api", "ownership", "apiPid", "tunnel", "errorCategory", "errorCode"):
                state.pop(key, None)
            state.update(health, updatedAt=time.time(), apiLaunches=supervisor.launches)
            write_json(status_path, state)
            if health != previous:
                with (config.state_dir / "events.jsonl").open("a", encoding="utf-8") as log:
                    log.write(json.dumps({"at": time.time(), **health}) + "\n")
                previous = health
            time.sleep(5)
    finally:
        supervisor.stop_owned_api()
        state.update(supervisor="stopped", api="stopped" if state.get("ownership") == "owned" else state.get("api"),
                     updatedAt=time.time())
        write_json(status_path, state)
        lock.close()
    return 0


def require_same_configuration(config, state):
    if (not state.get("configuration") or not same_path(state["configuration"], config.path)
            or state.get("configurationFingerprint") != config.fingerprint):
        raise RuntimeError("PREVIEW_CONFIGURATION_CONFLICT")


def start(config):
    current = status(config)
    if current.get("supervisor") == "running":
        require_same_configuration(config, current)
        return current
    if not os.environ.get("BIRDTIE_TENCENT_TOKENHUB_API_KEY"):
        raise RuntimeError("PREVIEW_RUNTIME_CREDENTIAL_MISSING")
    config.state_dir.mkdir(parents=True, exist_ok=True)
    child = durable_popen([sys.executable, str(Path(__file__).resolve()), "run", "--config", str(config.path)],
                          cwd=ROOT, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for _ in range(60):
        current = status(config)
        if current.get("supervisor") == "running":
            require_same_configuration(config, current)
            return current
        if child.poll() is not None:
            raise RuntimeError("PREVIEW_SUPERVISOR_START_FAILED")
        time.sleep(.25)
    raise RuntimeError("PREVIEW_SUPERVISOR_START_TIMEOUT")


def stop(config):
    path = config.state_dir / "status.json"
    if not path.is_file():
        return {"supervisor": "stopped"}
    state = read_json(path)
    if not state_matches_process(state):
        return status(config)
    write_json(config.state_dir / "stop.json", {"token": state["token"]})
    for _ in range(50):
        current = status(config)
        if current.get("supervisor") == "stopped":
            return current
        time.sleep(.25)
    return {**status(config), "stopRequested": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("start", "status", "stop", "run"))
    parser.add_argument("--config", default=str(DEFAULT_CONFIG))
    args = parser.parse_args()
    try:
        config = Config(args.config, validate=args.action in ("start", "run"))
        if args.action == "run":
            return run_supervisor(config)
        result = {"start": start, "status": status, "stop": stop}[args.action](config)
        print(json.dumps(result, indent=2))
        return 0
    except Exception as error:
        details = safe_error_details(error)
        allowed = details.get("errorCode", details["errorCategory"])
        print(json.dumps({"supervisor": "error", "error": allowed}), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
