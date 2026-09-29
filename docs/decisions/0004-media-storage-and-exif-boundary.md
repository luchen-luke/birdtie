# ADR 0004: Media storage and EXIF boundary

Date: 2026-09-30  
Status: Accepted as a provider-neutral contract; upload routes are not enabled.

## Decision

1. A Birdtie Session is required to create an upload intent. The server derives the owner from that Session. An intent names one random, opaque quarantine object key, declares a MIME type, byte limit and SHA-256 digest, and expires after a short interval. A client cannot choose a storage key or mark a media asset ready.
2. Quarantine, private originals and public derivatives are distinct storage scopes. The storage adapter signs only a bounded write to the quarantine key. It never returns a public or private read URL at intent creation. The API and workers use server credentials for quarantine and private reads.
3. A worker must stream the completed object from quarantine, enforce the byte limit, recompute SHA-256, inspect the signature and decoded media structure, reject excessive decoded dimensions, and obtain a clean malware-scan result. A scanner error is a rejection or retry, never approval. Only then may the original move to private storage and the record become `ready_private`.
4. EXIF is processed only after validation. Raw EXIF and precise coordinates are private metadata, encrypted with a separately managed key and never copied into City Graph public rows, search, logs, map tiles, Agent context or generic media responses. A user may later review a coarse place/time suggestion. No EXIF coordinate becomes public by default.
5. A public derivative is built by decoding, applying orientation, re-encoding and checking that sensitive metadata is absent. It requires a separate author confirmation tied to a target content object and revision, audience, location precision and rights check. A private upload alone cannot create a public variant. The existing `media_variants` table records only approved derivatives; no public variant route is open yet.
6. Media deletion or withdrawal first blocks new reads and signed URLs, then removes private originals, EXIF and derivatives according to a documented retention policy. A published Moment and its private source have separate lifecycles; removal of one does not silently rewrite the other.

## Adapter contract and state transitions

`internal/media/contract.go` defines the storage, scanner, metadata and derivative interfaces. Implementations must preserve opaque keys and make writes idempotent by upload intent ID. The API must verify every download against owner/Consent/target visibility at request time; possession of an object key is not authorization.

`intent_pending → quarantine → scanning → ready_private` is the success path. `rejected`, `expired` and `deleted` are terminal for that upload. An incomplete or uncertain scan cannot reach `ready_private`. Publication is a later object-specific action and does not change the original's private scope.

## Open deployment choices

The user has not selected an object-storage provider or data region. Scanner, key management, EXIF/orientation processor, signed URL behavior, retention period and backup/restore still need concrete providers and operational review. No upload endpoint or client picker is enabled until these are chosen and integrated. This avoids silently putting private photos on the API host or treating a local file adapter as production storage.

No Civu media code, OSS configuration or data was copied. The contract follows Birdtie's Memory/private-Media and public-Moment separation. See [OWASP File Upload Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html) for file validation, size limits, separated storage and malware scanning principles.
