"""Owned local API-process E2E. Synthetic instrumentation is not human acceptance."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]
API = ROOT / "apps/api"
WORK = ROOT / "work/v4-e2e001-local"
FRAME = ROOT / "work/v5-age038-resume/native-full-activity078-3"
BINARY = ROOT / "work/v5-age038-resume/phone-chat-binding1/birdtie-api078-community-city.exe"
BINARY_SHA = "0e5c68bcff43810644881198c5440145c1cd3c7d2e1092d5f95d7dd69aae7f8a"
UUID = re.compile(r"^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$")
DB = re.compile(r"^birdtie_social_e2e_[0-9a-f]{16}$")
NO_WINDOW = getattr(subprocess, "CREATE_NO_WINDOW", 0)


class Failure(Exception):
    """Only fixed, credential-free diagnostics may cross the evidence boundary."""


class UnknownMutation(Failure):
    pass


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def identity(value):
    if not isinstance(value, str) or not UUID.fullmatch(value):
        raise Failure("invalid_domain_id")
    return value


def owned_database(value):
    if not isinstance(value, str) or not DB.fullmatch(value):
        raise Failure("unowned_database")
    return value


def local_base(value):
    from urllib.parse import urlsplit
    u = urlsplit(value)
    try:
        port = u.port
    except ValueError:
        raise Failure("invalid_local_endpoint") from None
    if (u.scheme != "http" or u.hostname != "127.0.0.1" or not port
            or port in (4173, 3697) or u.username or u.password
            or u.path or u.query or u.fragment):
        raise Failure("invalid_local_endpoint")
    return value


def require(condition, code):
    if not condition:
        raise Failure(code)


def write(path, value):
    Path(path).write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def clean_environment(database, port):
    owned_database(database)
    system_keys = {"PATH", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP"}
    env = {k: v for k, v in os.environ.items() if k.upper() in system_keys}
    env.update(BIRDTIE_DATABASE_URL=f"postgres://birdtie:birdtie_local_only@127.0.0.1:55432/{database}?sslmode=disable",
               BIRDTIE_API_ADDR=f"127.0.0.1:{port}", BIRDTIE_DEV_PHONE_AUTH="true")
    # No OIDC, model provider, cognitive flag, scheduler or borrowed credential.
    return env


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


class Transport:
    def __init__(self, base, deadline, pid, records, opener=None):
        self.base = local_base(base)
        self.deadline, self.pid, self.records = deadline, pid, records
        self.opener = opener or urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def request(self, method, path, body=None, token=None, expected=(200,), phase="flow"):
        require(method in ("GET", "POST", "PUT", "DELETE"), "invalid_method")
        require(isinstance(path, str) and path.startswith("/v1/") and not any(c in path for c in "?#\r\n"), "invalid_route")
        remaining = self.deadline - time.monotonic()
        require(remaining > 0, "deadline_exceeded")
        request_id = "e2e_" + uuid.uuid4().hex
        headers = {"X-Request-ID": request_id}
        if token:
            headers["Authorization"] = "Bearer " + token
        raw = None if body is None else json.dumps(body).encode()
        if raw is not None:
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(self.base + path, data=raw, headers=headers, method=method)
        record = {"phase": phase, "pid": self.pid, "method": method,
                  "path": path, "requestID": request_id}
        self.records.append(record)
        try:
            try:
                response = self.opener.open(req, timeout=min(8, remaining))
            except urllib.error.HTTPError as e:
                response = e
            with response:
                record["status"] = response.status
                require(response.headers.get("X-Request-ID") == request_id, "request_trace_mismatch")
                payload = response.read(262145)
            require(len(payload) <= 262144, "response_too_large")
            require(record["status"] in expected, "unexpected_http_status")
            if not payload:
                return None
            result = json.loads(payload)
            require(isinstance(result, dict) and ("data" in result or record["status"] >= 400), "invalid_response")
            return result.get("data")
        except Failure:
            raise
        except (urllib.error.URLError, OSError, TimeoutError):
            record["result"] = "UNKNOWN_MUTATION" if method != "GET" else "READ_UNAVAILABLE"
            if method != "GET":
                raise UnknownMutation("mutation_result_unknown_no_automatic_retry") from None
            raise Failure("read_unavailable") from None
        except (ValueError, TypeError, AttributeError):
            record["result"] = "INVALID_RESPONSE"
            raise Failure("invalid_response") from None


class Runtime:
    def __init__(self, binary, expected_sha, database, out, deadline):
        self.binary = Path(binary).resolve()
        require(self.binary.is_relative_to(ROOT / "work"), "binary_outside_owned_workspace")
        require(re.fullmatch(r"[0-9a-f]{64}", expected_sha) is not None, "invalid_binary_digest")
        require(sha(self.binary) == expected_sha, "binary_digest_mismatch")
        self.expected_sha = expected_sha
        self.database = owned_database(database)
        self.out, self.deadline = Path(out), deadline
        self.marker = "Birdtie E2E001 LOCAL_SYNTHETIC " + uuid.uuid4().hex
        self.created = False
        self.process = None
        self.log_handle = None
        self.port = None
        self.generations = []

    def sql(self, text, db=None, log=None):
        database = db or self.database
        require(database == "postgres" or database == self.database, "unowned_sql_database")
        remaining = self.deadline - time.monotonic()
        require(remaining > 0, "deadline_exceeded")
        cmd = ["docker", "compose", "exec", "-T", "db", "psql", "-X", "-v", "ON_ERROR_STOP=1", "-qAt", "-U", "birdtie", "-d", database]
        try:
            p = subprocess.run(cmd, cwd=API, input=text.encode(), stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, timeout=min(90, remaining), creationflags=NO_WINDOW)
        except (OSError, subprocess.TimeoutExpired):
            raise Failure("owned_sql_process_unavailable") from None
        if log:
            write(self.out / (log + ".json"), {"command": cmd, "exitCode": p.returncode,
                                             "inputSHA256": hashlib.sha256(text.encode()).hexdigest(),
                                             "stdoutBytes": len(p.stdout), "stderrBytes": len(p.stderr)})
        require(p.returncode == 0, "owned_sql_failed")
        return p.stdout.decode("utf-8").strip()

    def prepare(self):
        require(not self.created, "database_already_created")
        write(self.out / "database-ownership.json", {"database": self.database, "marker": self.marker,
              "purpose": "BT-V4-E2E-001 LOCAL_SYNTHETIC", "createdByThisRunner": True})
        self.sql(f"CREATE DATABASE {self.database};", "postgres", "create")
        self.created = True
        self.sql(f"COMMENT ON DATABASE {self.database} IS '{self.marker}';", "postgres", "marker")
        baseline = FRAME / "baseline001-077.sql"
        up = FRAME / "source/apps/api/migrations/078_activity_participation_public_disclosure.sql"
        manifest = json.loads((FRAME / "source-before.json").read_text())
        for path, digest in manifest.items():
            require(sha(FRAME / "source" / path) == digest, "frozen_source_digest_mismatch")
        write(self.out / "frozen-api-provenance.json", {"binary": str(self.binary), "binarySHA256": self.expected_sha,
                    "sourceManifest": str(FRAME / "source-before.json"), "sourceManifestSHA256": sha(FRAME / "source-before.json"),
                    "sourceCount": len(manifest), "baselineSHA256": sha(baseline), "up078SHA256": sha(up),
                    "classification": "FROZEN078_LOCAL_SYNTHETIC_NOT_LIVE079"})
        self.sql(baseline.read_text(encoding="utf-8-sig"), log="baseline001-077")
        self.sql(up.read_text(encoding="utf-8-sig"), log="up078")

    def process_identity(self):
        require(self.process is not None and self.process.poll() is None, "owned_api_not_running")
        # CIM reads executable identity only; never CommandLine/env/phone files.
        script = f"$p=Get-CimInstance Win32_Process -Filter 'ProcessId={self.process.pid}'; if($null -eq $p){{exit 1}}; $p.ExecutablePath"
        p = subprocess.run(["powershell", "-NoProfile", "-Command", script], stdout=subprocess.PIPE,
                           stderr=subprocess.PIPE, timeout=10, creationflags=NO_WINDOW)
        require(p.returncode == 0 and Path(p.stdout.decode().strip()).resolve() == self.binary, "owned_api_path_mismatch")
        require(sha(self.binary) == self.expected_sha, "owned_api_digest_changed")

    def listener_owner(self):
        self.process_identity()
        script = f"@(Get-NetTCPConnection -State Listen -LocalAddress 127.0.0.1 -LocalPort {self.port} -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess) | ConvertTo-Json -Compress"
        p = subprocess.run(["powershell", "-NoProfile", "-Command", script], stdout=subprocess.PIPE,
                           stderr=subprocess.PIPE, timeout=10, creationflags=NO_WINDOW)
        require(p.returncode == 0, "listener_probe_failed")
        try:
            owners = json.loads(p.stdout.decode().strip() or "null")
        except ValueError:
            raise Failure("listener_probe_invalid") from None
        return owners == self.process.pid or owners == [self.process.pid]

    def start(self):
        require(self.process is None, "api_already_owned")
        require(self.created, "database_not_created")
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        require(port not in (4173, 3697), "reserved_phone_port")
        self.port = port
        require(sha(self.binary) == self.expected_sha, "binary_digest_mismatch")
        generation = len(self.generations) + 1
        self.log_handle = (self.out / f"api-process-{generation}.log").open("wb")
        self.process = subprocess.Popen([str(self.binary)], cwd=API, env=clean_environment(self.database, port),
                                        stdout=self.log_handle, stderr=subprocess.STDOUT, creationflags=NO_WINDOW)
        deadline = min(self.deadline, time.monotonic() + 20)
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        ready = False
        while time.monotonic() < deadline:
            require(self.process.poll() is None, "owned_api_exited_before_ready")
            try:
                with opener.open(f"http://127.0.0.1:{port}/readyz", timeout=1) as response:
                    ready = response.status == 200
                if ready:
                    break
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(.1)
        require(ready and self.listener_owner(), "readiness_not_owned_by_api")
        self.generations.append({"generation": generation, "pid": self.process.pid,
                                 "port": port, "executableSHA256": self.expected_sha, "readyz": 200,
                                 "listenerPIDVerified": True})
        return f"http://127.0.0.1:{port}"

    def stop(self):
        if self.process is None:
            return
        process = self.process
        if process.poll() is None:
            self.process_identity()
            require(self.listener_owner(), "refuse_stop_unverified_listener")
            process.terminate()  # Popen native process handle, never arbitrary PID kill.
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                raise Failure("owned_api_stop_timeout") from None
        if self.generations and self.generations[-1]["pid"] == process.pid:
            self.generations[-1]["stopped"] = True
            self.generations[-1]["exitCode"] = process.returncode
        self.process = None
        if self.log_handle:
            self.log_handle.close()
            self.log_handle = None

    def cleanup(self):
        # Cleanup has its own bounded time after operation deadline, no new flow.
        self.deadline = time.monotonic() + 40
        self.stop()
        if self.created:
            marker = self.sql(f"SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname='{self.database}';", "postgres")
            require(marker == self.marker, "refuse_drop_unowned_database_marker")
            self.sql(f"DROP DATABASE {self.database} WITH (FORCE);", "postgres", "drop")
            require(self.sql(f"SELECT count(*) FROM pg_database WHERE datname='{self.database}';", "postgres") == "0", "owned_database_not_dropped")
            self.created = False


def login(http, phone):
    http.request("POST", "/v1/auth/dev-phone/code", {"phone": phone}, phase="synthetic_login")
    result = http.request("POST", "/v1/auth/dev-phone/verify", {"phone": phone, "code": "123456"}, phase="synthetic_login")
    require(isinstance(result, dict) and isinstance(result.get("accessToken"), str) and result.get("tokenType") == "Bearer", "invalid_dev_session")
    token = result["accessToken"]
    account = http.request("GET", "/v1/me", token=token, phase="synthetic_login")
    return token, identity(account["id"])


def snapshot(runtime, conversation):
    identity(conversation)
    return json.loads(runtime.sql(f"""SELECT json_build_object(
      'messages',count(*),'rowHash',md5(COALESCE(string_agg(row_to_json(m)::text,E'\\n' ORDER BY m.id),'')))
      FROM conversation_messages m WHERE conversation_id='{conversation}';"""))


def verify_traces(out, processes, requests):
    require(len({r["requestID"] for r in requests}) == len(requests), "duplicate_request_trace_id")
    logs = {p["pid"]: (Path(out) / f"api-process-{p['generation']}.log").read_text(encoding="utf-8", errors="replace")
            for p in processes}
    for record in requests:
        require("status" in record and record["pid"] in logs, "request_trace_not_confirmed")
        needle = f"request_id={record['requestID']} method={record['method']} status={record['status']} "
        require(logs[record["pid"]].count(needle) == 1, "native_request_log_mismatch")
    return {"confirmedRequests": len(requests), "requestIDMethodStatusMatchExactlyOnce": True,
            "processGenerations": len(processes), "bodyTokenDSNRecorded": False}


def run_flow(runtime, requests):
    def current():
        return Transport(f"http://127.0.0.1:{runtime.port}", runtime.deadline, runtime.process.pid, requests)
    runtime.start()
    http = current()
    require(http.request("GET", "/v1/auth/dev-phone/status")["enabled"] is True, "dev_auth_not_enabled")
    a, a_id = login(http, "13800138082")
    b, b_id = login(http, "13800138083")
    require(a != b and a_id != b_id, "sessions_not_independent")
    for i, token in enumerate((a, b)):
        # Actual ordinary human Profile API, not a SQL public/verified bypass.
        http.request("PUT", "/v1/me/profile", {"displayName": f"本地合成 E2E 测试者 {i + 1}", "bio": "仅仪器验证，不是真实测试人员。", "visibility": "public"}, token, phase="synthetic_explicit_public_profile")
    require(http.request("GET", "/v1/me/ties", token=a) == [] and http.request("GET", "/v1/me/ties", token=b) == [], "unexpected_existing_ties")
    request = http.request("POST", "/v1/me/connection-requests", {"recipientAccountId": b_id, "scope": "friend", "note": "本地合成 E2E 自愿连接测试"}, a, (201,), "connect")
    request_id = identity(request["id"])
    http.request("POST", f"/v1/me/connection-requests/{request_id}/decision", {"action": "accept"}, b, phase="accept")
    ties = [http.request("GET", "/v1/me/ties", token=t, phase="accepted_bilateral") for t in (a, b)]
    require(all(len(items) == 1 for items in ties), "bilateral_tie_missing")
    tie_id = identity(ties[0][0]["id"])
    require(ties[1][0]["id"] == tie_id, "bilateral_tie_id_mismatch")
    runtime.stop()
    runtime.start()
    http = current()
    for token in (a, b):
        items = http.request("GET", "/v1/me/ties", token=token, phase="restart1_tie")
        require(len(items) == 1 and items[0]["id"] == tie_id, "restart_tie_not_persistent")
    chat = http.request("POST", f"/v1/me/ties/{tie_id}/conversation", token=a, phase="chat_start")
    conversation = identity(chat["id"])
    other = http.request("POST", f"/v1/me/ties/{tie_id}/conversation", token=b, phase="chat_start")
    require(other["id"] == conversation, "duplicate_conversation")
    message_ids = []
    for i, token in enumerate((a, b)):
        msg = http.request("POST", f"/v1/me/conversations/{conversation}/messages", {"body": f"本地合成人工消息 {i + 1}"}, token, (201,), "chat")
        message_ids.append(identity(msg["id"]))
    sources = {}
    # Domain selector is explicit and read from this owned synthetic DB only.
    for kind, table in (("place", "places"), ("activity", "activities")):
        target = runtime.sql(f"SELECT id::text FROM {table} ORDER BY id LIMIT 1;")
        sources[kind] = identity(target)
    source_hash = runtime.sql(f"SELECT md5((SELECT row_to_json(p)::text FROM places p WHERE id='{sources['place']}') || (SELECT row_to_json(a)::text FROM activities a WHERE id='{sources['activity']}'));")
    operations = []
    for kind, target in sources.items():
        before = http.request("GET", f"/v1/{'places' if kind == 'place' else 'activities'}/{target}", token=b, phase="current_source")
        require(before["id"] == target, "source_id_mismatch")
        operation = str(uuid.uuid4())
        body = {"operationId": operation, "entity": {"type": kind, "id": target}}
        receipt = http.request("POST", f"/v1/me/conversations/{conversation}/entity-shares", body, a, (201,), "share")
        entity = receipt["message"]["entity"]
        require(receipt["operationId"] == operation and entity["type"] == kind and entity["id"] == target and entity["available"] is True, "shared_card_mismatch")
        msg_id = identity(receipt["message"]["id"])
        message_ids.append(msg_id)
        replay = http.request("POST", f"/v1/me/conversations/{conversation}/entity-shares", body, a, (201,), "explicit_same_key_replay")
        require(replay["message"]["id"] == msg_id, "duplicate_share_message")
        operations.append({"type": kind, "entityID": target, "operationID": operation, "messageID": msg_id})
    before_restart = snapshot(runtime, conversation)
    require(before_restart["messages"] == 4, "wrong_message_count")
    runtime.stop()
    runtime.start()
    http = current()
    for token in (a, b):
        require(any(t["id"] == tie_id for t in http.request("GET", "/v1/me/ties", token=token, phase="restart2_tie")), "restart2_tie_missing")
        require(any(c["id"] == conversation for c in http.request("GET", "/v1/me/conversations", token=token, phase="restart2_chat")), "restart2_conversation_missing")
        messages = http.request("GET", f"/v1/me/conversations/{conversation}/messages", token=token, phase="restart2_messages")
        require(sorted(m["id"] for m in messages) == sorted(message_ids), "restart_messages_mismatch")
        for op in operations:
            require(any(m["id"] == op["messageID"] and m["entity"]["id"] == op["entityID"] and m["entity"]["available"] for m in messages), "receiver_card_unavailable")
            target = http.request("GET", f"/v1/{'places' if op['type'] == 'place' else 'activities'}/{op['entityID']}", token=token, phase="restart2_reopen_current")
            require(target["id"] == op["entityID"], "reopened_different_source")
    for op in operations:
        receipt = http.request("GET", f"/v1/me/conversations/{conversation}/entity-shares/{op['operationID']}", token=a, phase="restart2_keyed_receipt")
        require(receipt["message"]["id"] == op["messageID"], "restart_receipt_mismatch")
    after_restart = snapshot(runtime, conversation)
    require(before_restart == after_restart, "restart_mutated_original_messages")
    current_hash = runtime.sql(f"SELECT md5((SELECT row_to_json(p)::text FROM places p WHERE id='{sources['place']}') || (SELECT row_to_json(a)::text FROM activities a WHERE id='{sources['activity']}'));")
    require(source_hash == current_hash, "restart_mutated_sources")
    http.request("GET", f"/v1/me/conversations/{conversation}/messages", expected=(401,), phase="anonymous_denied")
    # Explicit human logout of A; B stays independent. No auto-refresh/re-auth.
    http.request("POST", "/v1/session/logout", token=a, expected=(204,), phase="explicit_logout")
    http.request("GET", f"/v1/me/conversations/{conversation}/messages", token=a, expected=(401,), phase="revoked_session_denied")
    http.request("GET", f"/v1/me/conversations/{conversation}/messages", token=b, phase="other_session_still_valid")
    # Isolated fixture visibility mutation is a negative scenario, never a
    # production publication/review. Already committed original rows stay intact.
    runtime.sql(f"UPDATE places SET publication_status='hidden' WHERE id='{sources['place']}';", log="synthetic-hide-place")
    http.request("GET", f"/v1/places/{sources['place']}", token=b, expected=(404,), phase="hidden_current_source_denied")
    http.request("POST", f"/v1/me/conversations/{conversation}/entity-shares", {"operationId": str(uuid.uuid4()), "entity": {"type": "place", "id": sources['place']}}, b, (404,), "hidden_new_share_denied")
    hidden = http.request("GET", f"/v1/me/conversations/{conversation}/messages", token=b, phase="hidden_original_card_redacted")
    place_message = next((m for m in hidden if m["id"] == operations[0]["messageID"]), None)
    require(place_message and place_message["entity"] == {"type": "place", "available": False}, "hidden_card_reference_leaked")
    require(snapshot(runtime, conversation) == after_restart, "failed_hidden_share_wrote_message")
    return {"syntheticActors": [a_id, b_id], "connectionRequestID": request_id, "tieID": tie_id,
            "conversationID": conversation, "messageIDs": message_ids, "shares": operations,
            "beforeRestart": before_restart, "afterRestart": after_restart,
            "sourceRowsUnchangedAcrossRestarts": True, "actualAPIRestarts": 2,
            "hiddenSyntheticSourceDeniedWithoutNewMessage": True}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--round", required=True)
    parser.add_argument("--api-binary", type=Path, default=BINARY)
    parser.add_argument("--api-sha256", default=BINARY_SHA)
    parser.add_argument("--deadline-seconds", type=int, default=300)
    args = parser.parse_args(argv)
    require(re.fullmatch(r"[a-z0-9][a-z0-9-]{0,47}", args.round) is not None, "invalid_round")
    require(60 <= args.deadline_seconds <= 600, "invalid_deadline")
    out = WORK / args.round
    out.mkdir(parents=True, exist_ok=False)
    database = "birdtie_social_e2e_" + uuid.uuid4().hex[:16]
    requests = []
    result = {"task": "BT-V4-E2E-001", "classification": "LOCAL_SYNTHETIC_INSTRUMENTATION_ONLY",
              "fullAcceptance": "NOT_PASSED_TWO_REAL_TESTERS_AND_REAL_AUTHORIZED_SOURCE_MISSING",
              "ClosedPilotReady": "NO", "ConsumerBeta": "NO", "databaseOwned": database,
              "live079Covered": False, "flowPass": False, "cleanupPass": False}
    runtime = None
    try:
        runtime = Runtime(args.api_binary, args.api_sha256, database, out, time.monotonic() + args.deadline_seconds)
        runtime.prepare()
        result["flow"] = run_flow(runtime, requests)
        result["flowPass"] = True
    except Failure as e:
        result["failure"] = str(e)
    except Exception:
        # Never serialize arbitrary exception/response/env: may contain a token.
        result["failure"] = "runner_unexpected_failure"
    finally:
        if runtime:
            try:
                runtime.cleanup()
                result["cleanupPass"] = True
            except Failure as e:
                result["cleanupFailure"] = str(e)
            except Exception:
                result["cleanupFailure"] = "runner_cleanup_unexpected_failure"
            result["processes"] = runtime.generations
        if result["flowPass"] and result["cleanupPass"]:
            try:
                result["nativeTraceCorrelation"] = verify_traces(out, runtime.generations, requests)
            except Failure as e:
                result["flowPass"] = False
                result["failure"] = str(e)
        write(out / "requests.json", requests)
        write(out / "result.json", result)
    print(json.dumps(result, ensure_ascii=False))
    return 0 if result["flowPass"] and result["cleanupPass"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
