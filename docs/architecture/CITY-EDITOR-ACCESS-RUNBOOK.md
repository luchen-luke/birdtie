# City editor access runbook

Date: 2026-09-30  
Status: Local operator procedure. No roles are seeded and no general role-management API is exposed.

`apps/api/cmd/city-editor-access` grants or revokes one City editor membership in a database transaction with an audit event. It runs only for a trusted operator with database access. The `operator` and target must be active Birdtie Accounts, and the City must be published. The ticket parameter is a non-sensitive approval reference, not a free-text reason or credential.

Before a grant, the operator must verify the target's real-world identity, City responsibility, role scope and approval ticket outside the application. A reviewer and submitter must be separate people; the API can only enforce separate Accounts. Do not give one person two identities to satisfy the review check. Reviewers must inspect source, rights, host and event time before publishing a candidate.

From `D:\Project\birdtie\apps\api`, after setting `BIRDTIE_DATABASE_URL` in the process environment:

```powershell
go run ./cmd/city-editor-access -action grant -city aberdeen-gb -account <target-account-uuid> -operator <operator-account-uuid> -role contributor -ticket OPS-1234
go run ./cmd/city-editor-access -action revoke -city aberdeen-gb -account <target-account-uuid> -operator <operator-account-uuid> -ticket OPS-1235
```

The first real Accounts require a configured OIDC issuer and completed sign-in. This command does not create Accounts, issue Sessions or bypass OIDC. Only run it against an environment with an approved operator workflow; preserve the external ticket and database audit log together. Rotate database credentials and review membership regularly. Current repository configuration is local only.
