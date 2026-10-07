# ADR 0016: Community social layer and constrained Activity organizer

Date: 2026-10-01  
Status: Accepted

## Context

The existing published Group/Community submission is a directory entry owned by one person, without membership. Activity publishing is Organization-only. The accepted identity model listed “communities” under Organization types, while ADR 0011 established direct owner publishing for Groups. Those statements do not express a self-organized persistent social Community.

## Decision

Extend the existing Community entity with its own membership and lifecycle. Community is separate from Organization and remains without account or Agent principal. Activity receives a relationally constrained Person XOR Community XOR Organization organizer; visibility and membership checks remain independent. Existing Organization Activities retain their Organization association and are backfilled. ADR 0011's owner confirmation and absence of City reviewer for existing Group submissions remain; Community ownership no longer means only a single `owner_account_id` field. An Organization whose historical type value is `community` remains an Organization.

This ADR narrows the 2026-09-30 identity model's sentence that treated all “communities” as Organizations; it does not alter the Organization principal or CityContext decisions. It extends ADR 0011 rather than erasing its historical publishing rule. Implementation progress and pilot readiness require separate evidence.

## Consequences

The server must authorize Community management and organizer actions against real membership. Migration must preserve existing Community and Activity identifiers and activity counts. Public Community Activities may be discovered and RSVP'd by nonmembers without joining the Community. Private and invite-only activities must not leak through list, detail or participation endpoints. No Community Agent, chat or map marker is introduced.
