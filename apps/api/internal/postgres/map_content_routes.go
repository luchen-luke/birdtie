package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"sort"
	"time"
)

// One bounded batch follows authoritative membership/display positions. Missing
// or unlocated members remain gaps; no author, proximity or array-order inference.
func mapContentRouteFacts(ctx context.Context, f humanWriteFrame, leaves []mapSpatialLeaf, store *Store, until *time.Time) (string, error) {
	ids := []string{}
	byMoment := map[string][]int{}
	for i, x := range leaves {
		if len(byMoment[x.pin.MomentID]) == 0 {
			ids = append(ids, x.pin.MomentID)
		}
		byMoment[x.pin.MomentID] = append(byMoment[x.pin.MomentID], i)
	}
	if len(ids) == 0 {
		return content.MomentControlHash([]any{}), nil
	}
	rows, e := f.tx.Query(ctx, `SELECT t.id::text,x.moment_id::text,CASE WHEN e.thread_id IS NULL THEN x.position ELSE d.display_position END,
 jsonb_build_array(t.xmin::text,t.version,t.status,x.xmin::text,e.xmin::text,e.version,d.xmin::text,d.version)::text
 FROM moment_threads t JOIN moment_thread_members x ON x.thread_id=t.id
 LEFT JOIN moment_thread_edit_states e ON e.thread_id=t.id
 LEFT JOIN moment_thread_display_members d ON d.thread_id=x.thread_id AND d.moment_id=x.moment_id
 WHERE t.status<>'withdrawn' AND(t.audience='PUBLIC' OR t.author_account_id=$2::uuid OR
 (t.version=1 AND e.thread_id IS NULL AND EXISTS(SELECT 1 FROM legacy_native_context_entities original
 JOIN legacy_native_context_runs run ON run.run_id=original.run_id
 JOIN legacy_import_runs imported ON imported.run_id=run.run_id AND imported.source_file_sha256=run.source_file_sha256 AND imported.manifest_file_sha256=run.manifest_file_sha256
 WHERE original.kind='thread' AND original.target_id=t.id AND original.owner_account_id=t.author_account_id)))
 AND x.thread_id IN(SELECT thread_id FROM moment_thread_members WHERE moment_id=ANY($1::uuid[]))
 AND(e.thread_id IS NULL OR d.display_position IS NOT NULL)
 ORDER BY t.id,3`, ids, f.a.ID)
	if e != nil {
		return "", content.ErrUnavailable
	}
	defer rows.Close()
	threads := map[string]map[int]string{}
	facts := []any{}
	for rows.Next() {
		var tid, mid, stamp string
		var pos int
		if rows.Scan(&tid, &mid, &pos, &stamp) != nil || pos < 0 || pos >= content.MomentThreadMaxEntries {
			return "", content.ErrUnavailable
		}
		if threads[tid] == nil {
			threads[tid] = map[int]string{}
		}
		threads[tid][pos] = mid
		facts = append(facts, []any{tid, mid, pos, stamp})
		for _, i := range byMoment[mid] {
			leaves[i].pin.ThreadID = tid
			leaves[i].pin.ThreadPosition = pos
		}
	}
	if rows.Err() != nil {
		return "", content.ErrUnavailable
	}
	rows.Close()
	for tid, nodes := range threads {
		for pos, mid := range nodes {
			next := nodes[pos+1]
			a, b := byMoment[mid], byMoment[next]
			// More than one legal geographic projection has no authoritative in-entry
			// order. Preserve all points, but do not choose an arbitrary route endpoint.
			if len(a) != 1 || len(b) != 1 {
				continue
			}
			from, to := &leaves[a[0]], &leaves[b[0]]
			if from.pin.ThreadID != tid || to.pin.ThreadID != tid {
				continue
			}
			from.pin.NextMarkerIDs = []string{to.pin.MarkerID}
		}
	}
	// Only the finite set of newest loaded candidates participates in the
	// bounded public Journey lookup. Unloaded endpoints never get bridged.
	candidateIDs := ids
	if len(candidateIDs) > 500 {
		candidateIDs = candidateIDs[:500]
	}
	journeyEdges := []JourneyPublicationEdge{}
	var err error
	if content.JourneysEnabled {
		journeyEdges, err = store.ReadJourneyPublicationEdgesTx(ctx, f.tx, content.PublicMomentAccess{Actor: f.a, SessionDigest: f.d}, candidateIDs, 200)
	}
	if err != nil {
		return "", err
	}
	for _, edge := range journeyEdges {
		from, to := []int{}, []int{}
		for _, i := range byMoment[edge.FromMomentID] {
			if leaves[i].pin.PlaceID == edge.FromPlaceID {
				from = append(from, i)
			}
		}
		for _, i := range byMoment[edge.ToMomentID] {
			if leaves[i].pin.PlaceID == edge.ToPlaceID {
				to = append(to, i)
			}
		}
		if len(from) != 1 || len(to) != 1 || from[0] == to[0] {
			continue
		}
		p := &leaves[from[0]].pin
		p.JourneyEdges = append(p.JourneyEdges, content.MapContentJourneyEdge{JourneyID: edge.JourneyID, PublicationRevision: edge.PublicationRevision, NextMarkerID: leaves[to[0]].pin.MarkerID, FromPosition: edge.FromPosition, ToPosition: edge.ToPosition, SourceVersion: edge.SourceStamp})
		shrinkMapLease(until, &edge.ValidUntil)
		facts = append(facts, []any{edge.JourneyID, edge.PublicationRevision, edge.FromMomentID, edge.ToMomentID, edge.FromPlaceID, edge.ToPlaceID, edge.FromPosition, edge.ToPosition, edge.SourceStamp})
	}
	for i := range leaves {
		sort.Slice(leaves[i].pin.JourneyEdges, func(a, b int) bool {
			x, y := leaves[i].pin.JourneyEdges[a], leaves[i].pin.JourneyEdges[b]
			if x.JourneyID != y.JourneyID {
				return x.JourneyID < y.JourneyID
			}
			return x.FromPosition < y.FromPosition
		})
	}
	for i := range leaves {
		p := &leaves[i].pin
		p.SourceVersion = content.MomentControlHash([]any{p.SourceVersion, p.ThreadID, p.ThreadPosition, p.NextMarkerIDs, p.JourneyEdges})
	}
	return content.MomentControlHash(facts), nil
}
