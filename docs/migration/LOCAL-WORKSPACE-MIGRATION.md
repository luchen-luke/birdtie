# Local workspace migration inventory

Date: 2026-09-29

## Workspace status

| Path | Observed state | Action |
|---|---|---|
| `D:\Project\birdtie` | Directory existed, empty; no `.git`, README, AGENTS.md, or docs directory | Initialized Git repository; created baseline README/AGENTS and requested docs structure |
| `D:\BirdTie` | Directory existed and contained no files or subdirectories | Inspected only; retained unchanged |
| `D:\Program\Civu` | Git repository on `master` tracking `origin/master`, with existing modified/untracked submodule/worktree/artifact state | Read-only inventory only; no files changed or copied |

## Copied materials

No files copied. `D:\BirdTie` was empty at inventory time, so there were no legacy Birdtie documents to classify or migrate.

| Source path | Destination path | Status |
|---|---|---|
| None | None | No eligible source files found |

No filename conflicts occurred. No same-name files were overwritten.

## Not migrated

- All Civu code, documentation, assets, and repository history: excluded by scope; Civu is a read-only reference source, not a migration source.
- No files from `D:\BirdTie`: none existed at inventory time.
- Any files subsequently added to `D:\BirdTie`: not part of this inventory; review individually before copying.

## Preservation notes

`D:\BirdTie` was not deleted or modified. `D:\Program\Civu` was not moved, deleted, or modified. `D:\Project\birdtie` is the canonical Birdtie repository after this setup. Any future Civu reuse must be evaluated in `docs/architecture/CIVU-REUSE-MATRIX.md` before implementation.

## Follow-up document consolidation (2026-09-29)

A search of the user's Codex Documents found three clearly named Birdtie deliverables in `C:\Users\chens\Documents\Codex\2026-09-29\referenced-chatgpt-conversation-this-is-an-2\outputs`. They were copied into the canonical repository without changing the source files:

| Source path | Destination path | Category |
|---|---|---|
| `C:\Users\chens\Documents\Codex\2026-09-29\referenced-chatgpt-conversation-this-is-an-2\outputs\01-Birdtie-产品文档-V3.0.md` | `D:\Project\birdtie\docs\product\01-Birdtie-产品文档-V3.0.md` | Product |
| `C:\Users\chens\Documents\Codex\2026-09-29\referenced-chatgpt-conversation-this-is-an-2\outputs\02-Birdtie-商业规划-V3.0.md` | `D:\Project\birdtie\docs\business\02-Birdtie-商业规划-V3.0.md` | Business |
| `C:\Users\chens\Documents\Codex\2026-09-29\referenced-chatgpt-conversation-this-is-an-2\outputs\03-Birdtie-技术架构-V1.0.md` | `D:\Project\birdtie\docs\architecture\03-Birdtie-技术架构-V1.0.md` | Architecture |

No destination conflict existed; no files were overwritten. Nearby Birdship-labeled deliverables were not copied because they are not the selected Birdtie documents. No other Birdtie-specific product/business/architecture/research files were found in `D:\BirdTie` (still empty) or the active task workspace. This inventory was a name-based search of accessible Documents, not an exhaustive scan of every drive or cloud project attachment.
