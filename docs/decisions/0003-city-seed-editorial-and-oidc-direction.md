# ADR 0003: City Seed editorial boundary and OIDC direction

Date: 2026-09-29  
Status: City Seed, provider-neutral OIDC server flow and Flutter web callback implemented locally; provider registration and real login validation pending.

## City Seed decision

1. Place submissions enter a private `place_candidates` queue. A submitter must hold an active city contributor or reviewer membership, derived from a valid Birdtie Session. Submission never creates a public Place.
2. A different Account with an active reviewer membership decides `publish`, `link_existing` or `reject`. The submitted source URL, rights note, expiry, decision note, reviewer and resolution are retained. The reviewer must check the source and rights outside the software; the API cannot establish legal permission from a URL alone. Separate Accounts alone do not prove independent human review.
3. `publish` creates one canonical Place, a source evidence record and a City Seed relation in one database transaction. `link_existing` points the candidate to a published Place in the same City; it may add an alias and a provider reference, but does not create a duplicate Place. A provider reference already mapped to another Place blocks the decision.
4. A later verified source can refresh the canonical Place's public source and expiry. All reviewed sources remain separate evidence rows. The public City/Place API continues to read only published objects, with source freshness visible.
5. Editorial memberships are provisioned by a trusted operator until organization role administration exists. No membership is seeded. The first provider account and city roles need a documented, audited operator process before real editorial use.

## OIDC decision

The first login integration uses generic OpenID Connect, as selected for Birdtie. The server implements Authorization Code with PKCE, one-time state and nonce, verified ID Token signature/issuer/audience/expiry, and Account mapping by the stable `issuer + subject` pair. Email or display name alone does not link Accounts. A newly verified identity creates a private Account/Profile. The callback delivers a short-lived Birdtie code to a fixed client URL; the client must redeem it with its own PKCE verifier before the server issues the opaque Session defined in ADR 0002.

The Flutter web callback checks its pending PKCE challenge, redeems the one-time Birdtie code, removes the callback query from browser history and keeps the Bearer credential only in running memory. A reload requires login again. An OIDC issuer, client registration, redirect URI and deployment secret handling still need configuration; native mobile/desktop callbacks remain separate work. The OIDC server endpoints are disabled without configuration, and no real provider login has been exercised. City Seed editorial routes also require audited role provisioning.

The implementation adds `github.com/coreos/go-oidc/v3` (Apache-2.0), `golang.org/x/oauth2` (BSD-style Go license), and transitive `github.com/go-jose/go-jose/v4` (Apache-2.0). These license identifications were read from the pinned modules' local `LICENSE` files. No Civu authentication code was copied.

References: [OpenID Connect Core](https://openid.net/specs/openid-connect-core-1_0-22.html), [Go OAuth2 PKCE API](https://pkg.go.dev/golang.org/x/oauth2).
