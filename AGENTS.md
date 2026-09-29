# Birdtie repository guidance

## Project identity and source of truth

- Birdtie is an **Agent-native local social network**.
- Birdtie is the next product version of Civu. It is rebuilt around Birdtie's own model in this repository, while existing users, data, interfaces and live services are migrated in stages. The existing store app identities are retained for the eventual update.
- `D:\Project\birdtie` is the only official Birdtie repository.
- `D:\Program\Civu` is a read-only reference source, not a working directory for Birdtie changes.
- `D:\BirdTie` is a legacy ChatGPT/Work folder. Preserve its contents; copy only explicitly selected materials and never delete or move files from it.

## Civu reuse boundary

- Before proposing or implementing any Civu reuse, review `docs/architecture/CIVU-REUSE-MATRIX.md` and update it when findings change.
- Never copy the entire Civu repository into Birdtie.
- Treat code reuse as a separate, explicit migration. Check dependencies, licenses, data ownership, privacy/security assumptions, external services, and architectural fit first.
- Keep Birdtie decisions and implementations native to Birdtie's Agent-native local-social-network goals.

## Documentation locations

Place documents in the matching directory under `docs/`: `product/`, `business/`, `architecture/`, `research/`, `decisions/`, or `migration/`. Record material copied from an external/legacy folder with its original source path and destination.
