"""Loopback-only synthetic organization map privacy acceptance."""

import json
import os
import secrets
import subprocess
import sys
import urllib.error
import urllib.request


BASE = os.environ.get("BIRDTIE_TEST_API_BASE", "http://127.0.0.1:3694")
if not BASE.startswith(("http://127.0.0.1:", "http://localhost:")):
    raise SystemExit("Only loopback development API is accepted")


def call(method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Content-Type": "application/json"} if data else {}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as error:
        raw = error.read()
        return error.code, json.loads(raw) if raw else {}


def expect(actual, wanted, label):
    if actual != wanted:
        raise SystemExit(f"FAIL {label}: {actual} != {wanted}")
    print(f"PASS {label}: HTTP {actual}")


def sql(statement):
    result = subprocess.run(
        ["docker", "compose", "exec", "-T", "db", "psql", "-U", "birdtie",
         "-d", "birdtie", "-v", "ON_ERROR_STOP=1", "-At", "-c", statement],
        cwd=os.path.join(os.path.dirname(__file__), "..", "apps", "api"),
        capture_output=True, text=True, check=True,
    )
    return result.stdout.strip()


def login():
    phone = "138" + f"{secrets.randbelow(100000000):08d}"
    expect(call("POST", "/v1/auth/dev-phone/code", {"phone": phone})[0], 200, "challenge")
    status, body = call("POST", "/v1/auth/dev-phone/verify", {"phone": phone, "code": "123456"})
    expect(status, 200, "local sign-in")
    token = body["data"]["accessToken"]
    status, body = call("GET", "/v1/me", token=token)
    expect(status, 200, "local identity")
    return token, body["data"]["id"]


owner, reviewer, stranger = login(), login(), login()
status, body = call("POST", "/v1/me/organizations",
                    {"name": "Birdtie 地图隐私本地验收", "organizationType": "club"}, owner[0])
expect(status, 201, "synthetic organization")
org = body["data"]["id"]
managed = f"/v1/me/organizations/{org}/map-location"
public = "/v1/cities/aberdeen-gb/organizations/map"


def absent(label):
    status, body = call("GET", public)
    expect(status, 200, label)
    if any(pin["id"] == org for pin in body["data"]):
        raise SystemExit("FAIL " + label + ": private coordinate leaked")


def present(label, latitude):
    status, body = call("GET", public)
    expect(status, 200, label)
    match = [pin for pin in body["data"] if pin["id"] == org]
    if len(match) != 1 or match[0]["latitude"] != latitude:
        raise SystemExit("FAIL " + label + ": reviewed point missing or changed")


absent("unlocated organization absent")
expect(call("GET", managed)[0], 401, "anonymous managed point denied")
expect(call("GET", managed, token=stranger[0])[0], 404, "nonmember point denied")
expect(call("PUT", managed, {"cityId": "aberdeen-gb", "latitude": 91, "longitude": -2}, owner[0])[0],
       400, "invalid point")
point = {"cityId": "aberdeen-gb", "latitude": 57.143, "longitude": -2.101}
expect(call("PUT", managed, point, stranger[0])[0], 403, "nonmember submit denied")
expect(call("PUT", managed, point, owner[0])[0], 200, "owner submits explicit point")
absent("unreviewed point absent")
expect(call("POST", managed + "/review", {"decision": "approve"}, owner[0])[0],
       403, "owner cannot self-approve")
expect(call("POST", managed + "/review", {"decision": "approve"}, stranger[0])[0],
       403, "stranger cannot review")
sql(f"INSERT INTO city_editor_memberships (city_id,account_id,role) "
    f"VALUES ('aberdeen-gb','{reviewer[1]}','reviewer') ON CONFLICT DO NOTHING")
expect(call("POST", managed + "/review", {"decision": "approve"}, reviewer[0])[0],
       204, "independent city reviewer approves")
absent("unverified organization absent")
sql(f"UPDATE organizations SET verification_status='verified' WHERE id='{org}'")
present("verified and approved point visible", 57.143)
sql(f"UPDATE organizations SET visibility='private' WHERE id='{org}'")
absent("private organization absent")
sql(f"UPDATE organizations SET visibility='public' WHERE id='{org}'")
expect(call("PUT", managed, {**point, "latitude": 57.144}, owner[0])[0],
       200, "coordinate edit resets review")
absent("edited point absent until review")
expect(call("POST", managed + "/review", {"decision": "reject"}, reviewer[0])[0],
       204, "reviewer rejects")
absent("rejected point absent")
expect(call("PUT", managed, {**point, "latitude": 57.144}, owner[0])[0],
       200, "owner resubmits")
expect(call("POST", managed + "/review", {"decision": "approve"}, reviewer[0])[0],
       204, "reviewer reapproves")
present("new approved point visible", 57.144)
expect(call("DELETE", managed, token=owner[0])[0], 204, "owner hides point")
absent("hidden point absent")
audit = sql(f"SELECT string_agg(action,',' ORDER BY occurred_at,id) "
            f"FROM organization_map_location_audit WHERE organization_id='{org}'")
if len(audit.split(",")) != 7:
    raise SystemExit("FAIL location audit count: " + audit)
print("PASS location audit: " + audit)
print("Synthetic local-only organization: " + org)
sql(f"UPDATE organizations SET verification_status='unverified', visibility='private' "
    f"WHERE id='{org}' AND name='Birdtie 地图隐私本地验收'")

if "--device-pin" in sys.argv:
    status, body = call("POST", "/v1/me/organizations",
                        {"name": "Birdtie 本地地图测试点（非真实组织）", "organizationType": "club"}, owner[0])
    expect(status, 201, "device-only synthetic organization")
    device_org = body["data"]["id"]
    endpoint = f"/v1/me/organizations/{device_org}/map-location"
    expect(call("PUT", endpoint, {"cityId": "aberdeen-gb", "latitude": 57.1497,
                                   "longitude": -2.0943}, owner[0])[0], 200, "device point submitted")
    expect(call("POST", endpoint + "/review", {"decision": "approve"}, reviewer[0])[0],
           204, "device point independently reviewed")
    # This mark exists only in the isolated local development database.
    sql(f"UPDATE organizations SET verification_status='verified' WHERE id='{device_org}'")
    status, body = call("GET", public)
    expect(status, 200, "device pin endpoint")
    if not any(pin["id"] == device_org for pin in body["data"]):
        raise SystemExit("FAIL synthetic device pin not in public response")
    print("LOCAL SYNTHETIC DEVICE PIN ID: " + device_org)
    print("After device inspection, reset this local-only organization to "
          "verification_status='unverified' and visibility='private'.")
