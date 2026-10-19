package postgres

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

// Only the stable thread membership (or its authoritative edited display
// order) defines this sequence. The map's existing ACL filters every member.
const mapContentThreadMembersSQL = `SELECT CASE WHEN e.thread_id IS NULL THEN x.position ELSE d.display_position END,m.id::text,jsonb_build_array(x.xmin::text,d.xmin::text,d.version,m.xmin::text,m.revision,m.status)::text FROM moment_thread_members x JOIN moments m ON m.id=x.moment_id LEFT JOIN moment_thread_edit_states e ON e.thread_id=x.thread_id LEFT JOIN moment_thread_display_members d ON d.thread_id=x.thread_id AND d.moment_id=x.moment_id WHERE x.thread_id=$1 AND(e.thread_id IS NULL OR d.display_position IS NOT NULL) ORDER BY 1 LIMIT 21`

func mapContentThreadFilter(ctx context.Context, f humanWriteFrame, ids []string) (*mapContentReadFilter, error) {
	v := &mapContentReadFilter{MomentIDs: append([]string{}, ids...), PlaceIDs: []string{}}
	rows, err := f.tx.Query(ctx, `SELECT DISTINCT place_id::text FROM(SELECT moment_id,place_id FROM moment_location_links WHERE status='ACTIVE' AND kind='PLACE' UNION ALL SELECT c.moment_id,p.place_id FROM private_moment_media_contexts c JOIN private_moment_media_context_places p ON p.context_id=c.id WHERE c.status='ACTIVE') r WHERE moment_id=ANY($1::uuid[]) ORDER BY 1`, ids)
	if err != nil {
		return nil, content.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return nil, content.ErrUnavailable
		}
		v.PlaceIDs = append(v.PlaceIDs, id)
	}
	if rows.Err() != nil {
		return nil, content.ErrUnavailable
	}
	sort.Strings(v.MomentIDs)
	return v, nil
}

func (s *Store) mapContentThreadTx(ctx context.Context, f humanWriteFrame, seed string) (content.MapContentThread, error) {
	v := content.MapContentThread{SchemaVersion: content.MapContentThreadSchema, ViewerAccountID: f.a.ID, SeedMomentID: seed, Status: "NOT_A_THREAD", Entries: []content.MapContentThreadEntry{}}
	var thread, stamp string
	err := f.tx.QueryRow(ctx, `SELECT t.id::text,jsonb_build_array(t.xmin::text,t.version,t.status,e.xmin::text,e.version)::text FROM moment_threads t JOIN moment_thread_members x ON x.thread_id=t.id LEFT JOIN moment_thread_edit_states e ON e.thread_id=t.id WHERE x.moment_id=$1 AND `+mapContentThreadIdentitySQL("t")+` AND `+mapContentThreadAccessSQL("t", "e", "$2"), seed, f.a.ID).Scan(&thread, &stamp)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return v, content.ErrUnavailable
	}
	ids, facts := []string{seed}, []any{seed, thread, stamp}
	ordered := []string{}
	if err == nil {
		rows, e := f.tx.Query(ctx, mapContentThreadMembersSQL, thread)
		if e != nil {
			return v, content.ErrUnavailable
		}
		for rows.Next() {
			var position int
			var id, source string
			if rows.Scan(&position, &id, &source) != nil || position != len(ordered) {
				rows.Close()
				return v, content.ErrUnavailable
			}
			ordered = append(ordered, id)
			facts = append(facts, []any{position, id, source})
		}
		e = rows.Err()
		rows.Close()
		if e != nil || len(ordered) < 1 || len(ordered) > content.MomentThreadMaxEntries {
			return v, content.ErrUnavailable
		}
		found := false
		for _, id := range ordered {
			if id == seed {
				found = true
			}
		}
		// A retired member must not discover the current siblings through an
		// old source ID, even if a stale map happens to still hold that ID.
		if !found {
			return v, content.ErrNotFound
		}
		ids = ordered
		v.Status, v.ThreadID = "AVAILABLE", thread
	}
	filter, err := mapContentThreadFilter(ctx, f, ids)
	if err != nil {
		return v, err
	}
	snapshot, err := readMapContentSnapshot(ctx, f, filter)
	if err != nil {
		return v, err
	}
	m := snapshot.moments[seed]
	if m == nil {
		return v, content.ErrNotFound
	}
	v.SeedRevision, v.SeedVisibilitySource = m.v.Revision, m.v.VisibilitySource
	v.ObservedAt, v.ValidUntil = snapshot.at, snapshot.until
	facts = append(facts, snapshot.source)
	for position, id := range ordered {
		entry := content.MapContentThreadEntry{Position: position, Status: "UNAVAILABLE"}
		if snapshot.moments[id] != nil {
			detail, e := s.mapContentMomentTx(ctx, f, &snapshot, id, true)
			if e != nil {
				return v, e
			}
			entry.Status, entry.Detail = "AVAILABLE", &detail
			shrinkMapLease(&v.ValidUntil, &detail.ValidUntil)
			facts = append(facts, detail.SourceVersion)
		}
		v.Entries = append(v.Entries, entry)
	}
	v.SourceVersion = content.MomentControlHash(facts)
	if !v.Valid(time.Now().UTC()) {
		return v, content.ErrUnavailable
	}
	return v, nil
}

type mapContentThreadProof struct {
	s            *Store
	d            [32]byte
	a            identity.Actor
	seed, source string
	until        time.Time
}

func (p *mapContentThreadProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.until.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	v, e := p.s.ReadHumanMapContentThread(ctx, p.d, p.a, p.seed)
	if e != nil {
		return e
	}
	if v.SourceVersion != p.source || v.ValidUntil.Before(p.until) {
		return identity.ErrControlChanged
	}
	return nil
}

func (s *Store) ReadHumanMapContentThread(ctx context.Context, d [32]byte, a identity.Actor, seed string) (content.MapContentThread, error) {
	if !identity.HumanControlUUID(seed) {
		return content.MapContentThread{}, content.ErrInvalid
	}
	ctx, f, err := s.beginMapContent(ctx, d, a)
	if err != nil {
		return content.MapContentThread{}, err
	}
	defer f.tx.Rollback(context.Background())
	v, err := s.mapContentThreadTx(ctx, f, seed)
	if err != nil {
		return v, err
	}
	err = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, v.ValidUntil, func() error {
		fresh, e := s.mapContentThreadTx(ctx, f, seed)
		if e != nil {
			return e
		}
		if fresh.SourceVersion != v.SourceVersion || fresh.ValidUntil.Before(v.ValidUntil) {
			return identity.ErrControlChanged
		}
		return nil
	}))
	if err != nil {
		return content.MapContentThread{}, err
	}
	v.ResponseProof = &mapContentThreadProof{s, d, a, seed, v.SourceVersion, v.ValidUntil}
	return v, nil
}
