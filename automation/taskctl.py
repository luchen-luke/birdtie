#!/usr/bin/env python3
import argparse, copy, hashlib, json, os, re, tempfile
from contextlib import contextmanager
from pathlib import Path, PureWindowsPath
from datetime import datetime, timezone

QUEUE = Path(__file__).with_name("codex_task_queue.json")
ROOT = Path(__file__).resolve().parents[1]
PRIORITY = {"P0":0, "P1":1, "P2":2, "P3":3}
# These are queue classifications, not authorization to deploy or enable a
# service. Unknown/gated classifications never inherit execution urgency.
DEPENDENCY_PRIORITY_GATES = {
    "Foundation", "Social Alpha", "Beta", "Business Pilot", "Aberdeen Closed Pilot"
}

VALID_STATUS = {"TODO", "IN_PROGRESS", "PARTIAL", "DONE", "BLOCKED"}
PARALLEL_SCOPES = {"CODE_AND_LOCAL_VERIFICATION",
                   "CODE_LOCAL_ONLY; source completion also requires -LIVE"}
MAX_PARALLEL = 3


class QueueData(dict):
    """Carry the read revision without adding fields to the queue document."""


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def identity(value):
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}", value):
        raise SystemExit("A nonempty coordinator/owner identifier is required")
    return value.casefold()


def normalize_write_scope(value):
    if not isinstance(value, str) or not value.strip() or value != value.strip():
        raise SystemExit("Write scope must be an explicit nonempty repository path")
    value = value.replace("\\", "/")
    if any(char in value for char in "*?[]{}") or any(ord(char) < 32 for char in value):
        raise SystemExit("Write scope cannot contain wildcards or control characters")
    windows_path = PureWindowsPath(value)
    if windows_path.drive and not windows_path.is_absolute():
        raise SystemExit("Write scope cannot be a drive-relative path")
    if ".." in value.split("/"):
        raise SystemExit("Write scope cannot contain path traversal")
    path = Path(value)
    try:
        resolved = (path if path.is_absolute() else ROOT / path).resolve()
    except (OSError, ValueError):
        raise SystemExit("Write scope is not a valid repository path")
    try:
        relative = resolved.relative_to(ROOT)
    except ValueError:
        raise SystemExit("Write scope must stay inside the Birdtie repository")
    if not relative.parts:
        raise SystemExit("Write scope cannot be the repository root")
    # Future files in a known repository area are allowed, but an invented
    # top-level area is not an explicit known write boundary.
    if not (ROOT / relative.parts[0]).exists():
        raise SystemExit("Write scope has an unknown repository area")
    return relative.as_posix().casefold()


def scope_conflict(left, right):
    left_parts, right_parts = left.split("/"), right.split("/")
    size = min(len(left_parts), len(right_parts))
    return left_parts[:size] == right_parts[:size]


def write_scopes(values):
    if not isinstance(values, list) or not values:
        raise SystemExit("At least one explicit --write-scope is required")
    normalized = [normalize_write_scope(value) for value in values]
    for index, left in enumerate(normalized):
        if any(scope_conflict(left, right) for right in normalized[index + 1:]):
            raise SystemExit("Duplicate or overlapping write scopes")
    return sorted(normalized)


def parallel_eligible(task):
    """Repository scheduling eligibility, never authority for live effects."""
    return (allows_dependency_priority(task)
            and ("completion_scope" not in task
                 or (isinstance(task["completion_scope"], str) and task["completion_scope"] in PARALLEL_SCOPES))
            and ("external_gate_ids" not in task or task["external_gate_ids"] == [])
            and ("activation_gates" not in task or task["activation_gates"] == [])
            and task.get("gate") in (None, "", *DEPENDENCY_PRIORITY_GATES)
            and task.get("release_stage") in (None, "")
            and not task.get("blocked_reason")
            and not task.get("partial_reason"))


def validate_parallel(data, active):
    metadata = data.get("parallel_execution")
    if "parallel_execution" not in data:
        if len(active) > 1:
            raise SystemExit("Multiple IN_PROGRESS tasks: " + ", ".join(active))
        return
    if not isinstance(metadata, dict) or set(metadata) != {"coordinator", "limit", "leases", "lease_history"}:
        raise SystemExit("Invalid parallel execution metadata")
    if identity(metadata["coordinator"]) != metadata["coordinator"]:
        raise SystemExit("Coordinator identifier must be normalized")
    limit = metadata["limit"]
    if isinstance(limit, bool) or not isinstance(limit, int) or not 1 <= limit <= MAX_PARALLEL:
        raise SystemExit("Parallel limit must be between 1 and 3")
    leases = metadata["leases"]
    if not isinstance(leases, dict) or not isinstance(metadata["lease_history"], list):
        raise SystemExit("Invalid parallel leases or lease history")
    if len(active) > limit:
        raise SystemExit("Parallel IN_PROGRESS limit exceeded")
    if len(active) > 1 and set(leases) != set(active):
        raise SystemExit("Every parallel IN_PROGRESS task needs an explicit lease")
    owners, boundaries = set(), []
    for task_id, lease in leases.items():
        if task_id not in active or not isinstance(lease, dict) or set(lease) != {"owner", "write_scope", "leased_at"}:
            raise SystemExit("Lease must refer to an IN_PROGRESS task")
        owner = identity(lease["owner"])
        if owner != lease["owner"] or owner in owners:
            raise SystemExit("Duplicate or non-normalized lease owner")
        owners.add(owner)
        task = task_map(data)[task_id]
        if not parallel_eligible(task) or not deps_done(task, task_map(data)):
            raise SystemExit("A parallel lease cannot bypass local execution gates")
        scopes = write_scopes(lease["write_scope"])
        if scopes != lease["write_scope"] or not isinstance(lease["leased_at"], str) or not lease["leased_at"].strip():
            raise SystemExit("Invalid normalized lease scope or timestamp")
        queue_scope = QUEUE.relative_to(ROOT).as_posix().casefold()
        if owner != metadata["coordinator"] and any(scope_conflict(scope, queue_scope) for scope in scopes):
            raise SystemExit("Only the coordinator owns the live queue write scope")
        if any(scope_conflict(left, right) for left in scopes for right in boundaries):
            raise SystemExit("Conflicting parallel write scopes")
        boundaries.extend(scopes)


@contextmanager
def writer_lock(path):
    """Persistent lock file; never unlink an inode another writer may hold."""
    with path.with_name(path.name + ".lock").open("a+b") as lock:
        if lock.seek(0, os.SEEK_END) == 0:
            lock.write(b"0")
            lock.flush()
        lock.seek(0)
        try:
            if os.name == "nt":
                import msvcrt
                msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
            else:
                import fcntl
                fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            raise SystemExit("Queue writer lock is held; retry from a fresh read")
        try:
            yield
        finally:
            lock.seek(0)
            if os.name == "nt":
                msvcrt.locking(lock.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def require_coordinator(data, coordinator):
    metadata = data.get("parallel_execution")
    if metadata is not None and (coordinator is None or identity(coordinator) != metadata["coordinator"]):
        raise SystemExit("Parallel queue mutations require the declared --coordinator")


def commit(path, data, updated, coordinator=None):
    require_coordinator(data, coordinator)
    expected = getattr(data, "source_digest", None) or digest(path)
    save(path, updated, expected_digest=expected)
    data.clear()
    data.update(updated)
    if isinstance(data, QueueData):
        data.source_digest = digest(path)


def release_lease(data, task_id, status):
    metadata = data.get("parallel_execution")
    if metadata is not None and task_id in metadata["leases"]:
        lease = metadata["leases"].pop(task_id)
        metadata["lease_history"].append(dict(lease, task_id=task_id, status=status,
                                              released_at=datetime.now(timezone.utc).isoformat()))

def validate(data):
    tasks = data.get("tasks")
    if not isinstance(tasks, list):
        raise SystemExit("Queue must contain a tasks list")
    ids = [task.get("id") for task in tasks]
    if len(ids) != len(set(ids)) or any(not task_id for task_id in ids):
        raise SystemExit("Queue has duplicate or empty task IDs")
    m = task_map(data)
    active = []
    for task in tasks:
        status = task.get("status")
        if status not in VALID_STATUS:
            raise SystemExit(f"Invalid status for {task['id']}: {status}")
        if status == "IN_PROGRESS":
            active.append(task["id"])
        if status == "DONE" and not str(task.get("evidence", "")).strip():
            raise SystemExit(f"DONE without evidence: {task['id']}")
        if status == "BLOCKED" and not str(task.get("blocked_reason", "")).strip():
            raise SystemExit(f"BLOCKED without reason: {task['id']}")
        if status == "PARTIAL" and not str(task.get("partial_reason", "")).strip():
            raise SystemExit(f"PARTIAL without reason: {task['id']}")
        for dependency in task.get("depends_on", []):
            if dependency not in m or dependency == task["id"]:
                raise SystemExit(f"Invalid dependency for {task['id']}: {dependency}")
    validate_parallel(data, active)
    visiting, visited = set(), set()
    def visit(task_id):
        if task_id in visiting:
            raise SystemExit(f"Dependency cycle at {task_id}")
        if task_id in visited:
            return
        visiting.add(task_id)
        for dependency in m[task_id].get("depends_on", []):
            visit(dependency)
        visiting.remove(task_id)
        visited.add(task_id)
    for task_id in ids:
        visit(task_id)

def load(path):
    contents = path.read_bytes()
    data = QueueData(json.loads(contents.decode("utf-8")))
    data.source_digest = hashlib.sha256(contents).hexdigest()
    validate(data)
    return data

def save(path, data, expected_digest=None):
    data["updated"] = datetime.now(timezone.utc).isoformat()
    validate(data)
    expected_digest = expected_digest or getattr(data, "source_digest", None) or digest(path)
    with writer_lock(path):
        if digest(path) != expected_digest:
            raise SystemExit("Queue changed after reading; reload before writing")
        with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent,
                                         prefix=path.name + ".", suffix=".tmp", delete=False) as tmp:
            tmp.write(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
            temporary = Path(tmp.name)
        try:
            if digest(path) != expected_digest:
                raise SystemExit("Queue changed after reading; reload before writing")
            os.replace(temporary, path)
        finally:
            temporary.unlink(missing_ok=True)

def task_map(data):
    return {t["id"]:t for t in data["tasks"]}

def deps_done(t, m):
    return all(m[d]["status"] == "DONE" for d in t.get("depends_on", []))

def get_task(data, tid):
    for t in data["tasks"]:
        if t["id"] == tid:
            return t
    raise SystemExit(f"Unknown task: {tid}")

def cmd_summary(data):
    counts={}
    for t in data["tasks"]:
        counts[t["status"]]=counts.get(t["status"],0)+1
    print("Task summary")
    for k in sorted(counts): print(f"  {k}: {counts[k]}")
    print(f"  TOTAL: {len(data['tasks'])}")
    for label, selected in (("P0", [t for t in data["tasks"] if t.get("priority") == "P0"]),
                            ("V4", [t for t in data["tasks"] if t["id"].startswith("BT-V4-")])):
        detail = ", ".join(f"{status}={sum(t['status'] == status for t in selected)}"
                           for status in sorted(VALID_STATUS) if any(t["status"] == status for t in selected))
        print(f"  {label}: {len(selected)} ({detail})")

def allows_dependency_priority(task):
    if task.get("priority") not in PRIORITY:
        return False
    if task.get("completion_scope") == "LIVE_ONLY":
        return False
    gate = task.get("gate")
    if gate and (not isinstance(gate, str) or gate not in DEPENDENCY_PRIORITY_GATES):
        return False
    # V4_POST_PILOT is the sole release_stage currently in the queue. Do not
    # infer authorization from it, or from any future unrecognized stage.
    if task.get("release_stage"):
        return False
    return True

def ready_p0_dependency_priorities(data, m, parallel=False):
    """Return ready P1 prerequisites of an entirely actionable P0 TODO closure.

    The validated queue is read-only here. BLOCKED/PARTIAL/external/live/stage
    gates cannot become executable through a dependency declaration. Only the
    temporary candidate order changes; declared priority and phase are retained.
    """
    promoted = set()
    for root in data["tasks"]:
        if root["status"] != "TODO" or root.get("priority") != "P0":
            continue
        pending = [root["id"]]
        unfinished, seen = set(), set()
        actionable = True
        while pending:
            task_id = pending.pop()
            if task_id in seen:
                continue
            seen.add(task_id)
            task = m[task_id]
            if task["status"] == "DONE":
                continue
            if (task["status"] != "TODO" or not allows_dependency_priority(task)
                    or (parallel and not parallel_eligible(task))):
                actionable = False
                break
            unfinished.add(task_id)
            pending.extend(task.get("depends_on", []))
        if actionable:
            promoted.update(task_id for task_id in unfinished
                            if m[task_id].get("priority") == "P1" and deps_done(m[task_id], m))
    return promoted

def ordered_candidates(data, parallel=False):
    m = task_map(data)
    promoted = ready_p0_dependency_priorities(data, m, parallel=parallel)
    candidates = []
    for index, task in enumerate(data["tasks"]):
        if task["status"] == "TODO" and deps_done(task, m) and (not parallel or parallel_eligible(task)):
            inherited = task["id"] in promoted
            candidates.append((0 if inherited else PRIORITY.get(task.get("priority", "P9"), 9),
                               0 if inherited else 1, index, task))
    return [candidate[3] for candidate in sorted(candidates, key=lambda candidate: candidate[:3])]


def cmd_next(data, parallel=False):
    if parallel:
        metadata = data.get("parallel_execution")
        active = [task["id"] for task in data["tasks"] if task["status"] == "IN_PROGRESS"]
        print(json.dumps({"candidates": ordered_candidates(data, parallel=True),
                          "active_count": len(active), "limit": metadata["limit"] if metadata else None,
                          "unleased_active": [task_id for task_id in active
                                              if not metadata or task_id not in metadata["leases"]]},
                         ensure_ascii=False, indent=2))
        return
    active=[t for t in data["tasks"] if t["status"] == "IN_PROGRESS"]
    if active:
        print("Finish or block the current task before claiming another:")
        print(json.dumps(active[0], ensure_ascii=False, indent=2))
        return
    candidates = ordered_candidates(data)
    if not candidates:
        print("No executable TODO task. Check BLOCKED/IN_PROGRESS/dependencies.")
        return
    t=candidates[0]
    print(json.dumps(t, ensure_ascii=False, indent=2))

def cmd_parallel_config(path, data, coordinator, limit):
    coordinator = identity(coordinator)
    if isinstance(limit, bool) or not isinstance(limit, int) or not 1 <= limit <= MAX_PARALLEL:
        raise SystemExit("Parallel limit must be between 1 and 3")
    require_coordinator(data, coordinator)
    updated = copy.deepcopy(data)
    if "parallel_execution" not in updated:
        updated["parallel_execution"] = {"coordinator": coordinator, "limit": limit,
                                         "leases": {}, "lease_history": []}
    else:
        updated["parallel_execution"]["limit"] = limit
    commit(path, data, updated, coordinator)
    print(f"Configured parallel limit {limit}; coordinator {coordinator}")


def add_lease(data, task_id, owner, scopes):
    metadata = data.get("parallel_execution")
    if metadata is None:
        raise SystemExit("Configure parallel execution before registering a lease")
    if task_id in metadata["leases"]:
        raise SystemExit("Task already has a lease")
    owner, scopes = identity(owner), write_scopes(scopes)
    queue_scope = QUEUE.relative_to(ROOT).as_posix().casefold()
    if owner != metadata["coordinator"] and any(scope_conflict(scope, queue_scope) for scope in scopes):
        raise SystemExit("Only the coordinator owns the live queue write scope")
    metadata["leases"][task_id] = {"owner": owner, "write_scope": scopes,
                                    "leased_at": datetime.now(timezone.utc).isoformat()}


def cmd_lease(path, data, tid, owner, scopes, coordinator):
    require_coordinator(data, coordinator)
    if get_task(data, tid)["status"] != "IN_PROGRESS":
        raise SystemExit("Only an existing IN_PROGRESS task can receive a lease")
    updated = copy.deepcopy(data)
    add_lease(updated, tid, owner, scopes)
    commit(path, data, updated, coordinator)
    print(f"Registered lease for {tid}")


def cmd_start(path, data, tid, parallel=False, owner=None, scopes=None, coordinator=None):
    require_coordinator(data, coordinator)
    m=task_map(data); t=get_task(data,tid)
    active=[other["id"] for other in data["tasks"] if other["status"] == "IN_PROGRESS"]
    if active and not parallel:
        raise SystemExit("Task already IN_PROGRESS: "+", ".join(active))
    if not parallel and (owner is not None or scopes is not None):
        raise SystemExit("--owner/--write-scope require --parallel")
    if parallel:
        metadata = data.get("parallel_execution")
        if metadata is None:
            raise SystemExit("Configure parallel execution before parallel start")
        if len(active) >= metadata["limit"]:
            raise SystemExit("Parallel IN_PROGRESS limit reached")
        if any(task_id not in metadata["leases"] for task_id in active):
            raise SystemExit("Register explicit leases for existing IN_PROGRESS tasks first")
        if t["status"] != "TODO" or not parallel_eligible(t):
            raise SystemExit("Parallel start requires an ungated local TODO task")
    if t["status"] not in {"TODO","BLOCKED","PARTIAL"}:
        raise SystemExit(f"Cannot start {tid}: status={t['status']}")
    missing=[d for d in t.get("depends_on",[]) if m[d]["status"]!="DONE"]
    if missing:
        raise SystemExit("Dependencies not DONE: "+", ".join(missing))
    updated = copy.deepcopy(data)
    t = get_task(updated, tid)
    t["status"]="IN_PROGRESS"; t["started_at"]=datetime.now(timezone.utc).isoformat()
    t.pop("blocked_reason",None)
    t.pop("partial_reason",None)
    if parallel:
        add_lease(updated, tid, owner, scopes)
    commit(path,data,updated,coordinator); print(f"Started {tid}")

def cmd_done(path, data, tid, evidence, coordinator=None):
    require_coordinator(data, coordinator)
    t=get_task(data,tid)
    if t["status"] != "IN_PROGRESS":
        raise SystemExit(f"Cannot complete {tid}: status={t['status']}; start it first")
    if not evidence.strip():
        raise SystemExit("Evidence is required to mark DONE")
    updated = copy.deepcopy(data); t = get_task(updated, tid)
    t["status"]="DONE"; t["completed_at"]=datetime.now(timezone.utc).isoformat(); t["evidence"]=evidence.strip()
    t.pop("partial_reason",None)
    release_lease(updated, tid, "DONE")
    commit(path,data,updated,coordinator); print(f"Completed {tid}")

def cmd_block(path, data, tid, reason, coordinator=None):
    require_coordinator(data, coordinator)
    t=get_task(data,tid)
    if not reason.strip(): raise SystemExit("Reason required")
    if t["status"] == "DONE": raise SystemExit("Reopen DONE task before blocking it")
    updated = copy.deepcopy(data); t = get_task(updated, tid)
    t["status"]="BLOCKED"; t["blocked_reason"]=reason.strip()
    t.pop("partial_reason",None)
    release_lease(updated, tid, "BLOCKED")
    commit(path,data,updated,coordinator); print(f"Blocked {tid}")

def cmd_partial(path, data, tid, reason, coordinator=None):
    require_coordinator(data, coordinator)
    t=get_task(data,tid)
    if not reason.strip(): raise SystemExit("Reason required")
    if t["status"] == "DONE": raise SystemExit("Reopen DONE task before marking partial")
    updated = copy.deepcopy(data); t = get_task(updated, tid)
    t["status"]="PARTIAL"; t["partial_reason"]=reason.strip()
    t.pop("blocked_reason",None)
    release_lease(updated, tid, "PARTIAL")
    commit(path,data,updated,coordinator); print(f"Partial {tid}")

def cmd_reset(path, data, tid, coordinator=None):
    require_coordinator(data, coordinator)
    updated = copy.deepcopy(data); t=get_task(updated,tid)
    t["status"]="TODO"
    for k in ["started_at","completed_at","evidence","blocked_reason","partial_reason"]: t.pop(k,None)
    release_lease(updated, tid, "TODO")
    commit(path,data,updated,coordinator); print(f"Reset {tid}")

def main():
    p=argparse.ArgumentParser()
    p.add_argument("--queue", type=Path, default=QUEUE,
                   help="queue file; use an isolated copy for workflow checks")
    sp=p.add_subparsers(dest="cmd",required=True)
    sp.add_parser("summary"); n=sp.add_parser("next"); n.add_argument("--parallel", action="store_true"); sp.add_parser("validate")
    c=sp.add_parser("parallel-config"); c.add_argument("--coordinator", required=True); c.add_argument("--limit", type=int, required=True)
    lease=sp.add_parser("lease"); lease.add_argument("id"); lease.add_argument("--owner", required=True); lease.add_argument("--write-scope", action="append", required=True); lease.add_argument("--coordinator", required=True)
    s=sp.add_parser("start"); s.add_argument("id"); s.add_argument("--parallel", action="store_true"); s.add_argument("--owner"); s.add_argument("--write-scope", action="append")
    d=sp.add_parser("done"); d.add_argument("id"); d.add_argument("--evidence",required=True)
    b=sp.add_parser("block"); b.add_argument("id"); b.add_argument("--reason",required=True)
    f=sp.add_parser("partial"); f.add_argument("id"); f.add_argument("--reason",required=True)
    r=sp.add_parser("reset"); r.add_argument("id")
    for mutation in (s, d, b, f, r):
        mutation.add_argument("--coordinator", help="declared root writer for a parallel-configured queue")
    a=p.parse_args(); data=load(a.queue)
    if a.cmd=="summary": cmd_summary(data)
    elif a.cmd=="next": cmd_next(data,a.parallel)
    elif a.cmd=="validate": print(f"Queue valid: {len(data['tasks'])} tasks")
    elif a.cmd=="parallel-config": cmd_parallel_config(a.queue,data,a.coordinator,a.limit)
    elif a.cmd=="lease": cmd_lease(a.queue,data,a.id,a.owner,a.write_scope,a.coordinator)
    elif a.cmd=="start": cmd_start(a.queue,data,a.id,a.parallel,a.owner,a.write_scope,a.coordinator)
    elif a.cmd=="done": cmd_done(a.queue,data,a.id,a.evidence,a.coordinator)
    elif a.cmd=="block": cmd_block(a.queue,data,a.id,a.reason,a.coordinator)
    elif a.cmd=="partial": cmd_partial(a.queue,data,a.id,a.reason,a.coordinator)
    elif a.cmd=="reset": cmd_reset(a.queue,data,a.id,a.coordinator)
if __name__=="__main__": main()
