# ADR 0002: Session and profile consent boundary

Date: 2026-09-29  
Status: Accepted for local Foundation implementation; identity-provider integration is pending.

## Decision

1. Birdtie maps a verified external issuer/subject to an Account. The generic OIDC callback validates the provider ID Token, and a one-time PKCE-bound Birdtie exchange code is required before Session issuance. No public route accepts an Account ID, issuer, subject or arbitrary token as proof of identity. Account recovery and provider-specific onboarding remain open.
2. Session credentials are opaque 32-byte random values. PostgreSQL stores only SHA-256 digests. A session has an eight-hour absolute limit and a rolling thirty-minute idle limit. Every authenticated request rechecks session validity and active Account status in the primary database. Logout revokes the session there.
3. Bearer credentials travel only through the Authorization header. Production transport requires HTTPS. Browser storage, refresh, device management and identity-provider choices remain open; do not put bearer credentials in URL parameters or browser local storage.
4. The first private resource is a Profile. A private Profile is visible to its owner or a named recipient with a current `profile_view/read` Consent grant. An authenticated blocked actor is denied even if the Profile is public or the grant remains. Missing and inaccessible Profiles have the same 404 response.
5. An authenticated owner can grant Profile read access to one active Account for a bounded expiry of one minute to ninety days, list own grants and revoke a grant. The owner is always derived from the session. Grant and revocation writes are transactional and recorded in `audit_events`. No generic Consent writer is exposed for Media, Memory or Agent context.
6. Anonymous visitors can read explicitly public Profiles. A block cannot prevent an anonymous visitor from seeing a public Profile; public visibility must therefore be chosen with that expectation. Private Profiles do not appear in public City/Place routes.

## Consequences

- `003_sessions_and_profile_consent.sql` introduces provider identity mappings, hashed sessions, block edges and a unique active grant boundary. Existing Account/Profile/Consent tables remain Birdtie-owned.
- The OIDC handoff can create a private Account/Profile for a newly verified issuer/subject. Flutter web implements a callback with an in-memory Bearer credential; reload requires login. Native callbacks and any future renewal or persistent secure storage need separate design.
- Media/Memory ACLs, report management, session audit events, rate limits, pagination and private read audit policy remain separate work. Account block/unblock now revokes grants between the two Accounts; authenticated Profile and Account-hosted Activity reads enforce the block. Future social queries must use the same rule. The presence of these tables does not authorize opening unrelated routes.
- The local API may bind to loopback without TLS. Any external deployment must terminate HTTPS and complete its privacy/security review.

## Security references

- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [OWASP REST Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/REST_Security_Cheat_Sheet.html)
