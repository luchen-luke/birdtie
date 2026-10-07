"""Run a synthetic, loopback-only organization membership acceptance path."""

import json
import os
import secrets
import urllib.error
import urllib.request


base = os.environ.get("BIRDTIE_TEST_API_BASE", "http://127.0.0.1:3694")
if not base.startswith(("http://127.0.0.1:", "http://localhost:")):
    raise SystemExit("Only a loopback development API is accepted")


def call(method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Content-Type": "application/json"} if data else {}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as error:
        raw = error.read()
        return error.code, json.loads(raw) if raw else {}


def expect(status, wanted, name):
    if status != wanted:
        raise SystemExit(f"FAIL {name}: HTTP {status}, expected {wanted}")
    print(f"PASS {name}: HTTP {status}")


users = []
for _ in range(3):
    phone = "138" + f"{secrets.randbelow(100000000):08d}"
    expect(call("POST", "/v1/auth/dev-phone/code", {"phone": phone})[0], 200, "local challenge")
    status, result = call("POST", "/v1/auth/dev-phone/verify", {"phone": phone, "code": "123456"})
    expect(status, 200, "local sign-in")
    token = result["data"]["accessToken"]
    status, result = call("GET", "/v1/me", token=token)
    expect(status, 200, "person identity")
    users.append((token, result["data"]["id"]))

a, b, c = users
status, result = call("POST", "/v1/me/organizations",
                      {"name": "Birdtie 本地成员验收", "organizationType": "club"}, a[0])
expect(status, 201, "create synthetic organization")
org = result["data"]["id"]
members = f"/v1/me/organizations/{org}/members"
expect(call("GET", members)[0], 401, "anonymous roster")
expect(call("GET", members, token=b[0])[0], 403, "nonmember roster")
status, result = call("GET", members, token=a[0])
expect(status, 200, "owner roster")
owner_membership = result["data"][0]["id"]
status, result = call("POST", members, {"userAccountId": b[1], "role": "member"}, a[0])
expect(status, 201, "owner invite")
b_membership = result["data"]["id"]
expect(call("POST", members, {"userAccountId": b[1], "role": "member"}, a[0])[0],
       409, "duplicate invite")
invite = f"/v1/me/organization-invitations/{b_membership}/accept"
expect(call("POST", invite, token=c[0])[0], 403, "other person cannot accept")
status, result = call("GET", "/v1/me/organization-invitations", token=b[0])
expect(status, 200, "target inbox invitation")
if len(result["data"]) != 1 or result["data"][0]["id"] != b_membership:
    raise SystemExit("FAIL invite target list")
expect(call("POST", invite, token=b[0])[0], 200, "target accepts")
expect(call("GET", members, token=b[0])[0], 403, "ordinary member roster denied")
expect(call("POST", members, {"userAccountId": c[1], "role": "member"}, b[0])[0],
       403, "ordinary member invite denied")
expect(call("PUT", f"{members}/{owner_membership}/role", {"role": "admin"}, a[0])[0],
       409, "last owner cannot demote")
expect(call("DELETE", f"{members}/{owner_membership}", token=a[0])[0],
       409, "last owner cannot leave")
expect(call("PUT", f"{members}/{b_membership}/role", {"role": "admin"}, a[0])[0],
       200, "owner promotes admin")
expect(call("PUT", f"{members}/{owner_membership}/role", {"role": "member"}, b[0])[0],
       403, "admin cannot demote owner")
status, result = call("POST", members, {"userAccountId": c[1], "role": "member"}, b[0])
expect(status, 201, "admin invites member")
c_membership = result["data"]["id"]
expect(call("POST", f"/v1/me/organization-invitations/{c_membership}/accept", token=c[0])[0],
       200, "second target accepts")
expect(call("PUT", f"{members}/{b_membership}/role", {"role": "owner"}, a[0])[0],
       200, "owner transfer adds second owner")
expect(call("PUT", f"{members}/{owner_membership}/role", {"role": "admin"}, a[0])[0],
       200, "original owner steps down")
expect(call("DELETE", f"{members}/{owner_membership}", token=b[0])[0],
       204, "new owner revokes old admin")
expect(call("GET", members, token=a[0])[0], 403, "revoked account loses access")
status, result = call("POST", "/v1/me/organizations",
                      {"name": "Birdtie 第二个本地验收组织", "organizationType": "club"}, c[0])
expect(status, 201, "other organization")
other_org = result["data"]["id"]
expect(call("GET", f"/v1/me/organizations/{other_org}/members", token=b[0])[0],
       403, "cross-organization roster denied")
expect(call("PUT", f"/v1/me/organizations/{other_org}/members/{c_membership}/role",
            {"role": "admin"}, c[0])[0], 403, "cross-organization membership ID denied")
print(f"Synthetic organization ID for audit check: {org}")
for token, _ in users:
    call("POST", "/v1/session/logout", token=token)
