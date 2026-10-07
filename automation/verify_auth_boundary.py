"""Check local API session and ownership boundaries with development identities.

This cannot verify an external OIDC provider or production identity.
"""

import json
import os
import urllib.error
import urllib.request


base = os.environ.get("BIRDTIE_TEST_API_BASE", "http://127.0.0.1:3694")
if not base.startswith(("http://127.0.0.1:", "http://localhost:")):
    raise SystemExit("Only a loopback development API is accepted")


def request(method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Content-Type": "application/json"} if data else {}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as error:
        raw = error.read()
        return error.code, json.loads(raw) if raw else {}


def require(actual, expected, label):
    if actual != expected:
        raise SystemExit(f"{label}: expected {expected}, got {actual}")
    print(f"PASS {label}: HTTP {actual}")


require(request("GET", "/v1/me")[0], 401, "anonymous /me")
tokens = []
ids = []
for phone in ("13800138041", "13800138042"):
    require(request("POST", "/v1/auth/dev-phone/code", {"phone": phone})[0], 200,
            "local challenge")
    status, result = request("POST", "/v1/auth/dev-phone/verify",
                             {"phone": phone, "code": "123456"})
    require(status, 200, "local verification")
    token = result["data"]["accessToken"]
    tokens.append(token)
    status, result = request("GET", "/v1/me", token=token)
    require(status, 200, "valid session")
    ids.append(result["data"]["id"])
if ids[0] == ids[1]:
    raise SystemExit("distinct development identities share one account")
print("PASS distinct server-issued account IDs")
require(request("GET", f"/v1/accounts/{ids[1]}/profile", token=tokens[0])[0],
        404, "private profile ownership")
require(request("POST", "/v1/session/logout", token=tokens[0])[0],
        204, "logout revocation")
require(request("GET", "/v1/me", token=tokens[0])[0],
        401, "revoked session")
require(request("GET", "/v1/me", token=tokens[1])[0],
        200, "other session remains valid")
require(request("POST", "/v1/session/logout", token=tokens[1])[0],
        204, "other logout")
