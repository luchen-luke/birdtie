package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"time"
)

// Only the currently authorized moment subset is joined. Profile visibility
// remains independent: seeing a moment does not grant access to a private name
// or avatar. Binary avatars reuse their existing current-authority endpoint.
func readMapContentAuthors(ctx context.Context, f humanWriteFrame, v *mapContentSnapshot) ([]any, error) {
	facts := []any{}
	if len(v.order) == 0 {
		return facts, nil
	}
	rows, e := f.tx.Query(ctx, `SELECT m.id::text,a.id::text,
 CASE WHEN a.id=$2::uuid OR(birdtie_profile_visible_to(a.id,$2::uuid) AND birdtie_agent_profile_field_allowed(a.id,$2::uuid,'displayName'))
 THEN coalesce(nullif(p.display_name,''),nullif(a.handle,''),'Birdtie 成员') ELSE 'Birdtie 成员' END,
 birdtie_person_avatar_metadata(a.id,$2::uuid),
 CASE WHEN x.run_id IS NULL THEN 'NATIVE' WHEN n.id IS NOT NULL AND m.created_at<r.created_at THEN 'CIVU_ORIGINAL' ELSE 'UNKNOWN' END,
 CASE WHEN m.status='published' AND m.visibility='public' THEN m.published_at END,
 least((SELECT min(c.expires_at) FROM communities c WHERE c.expires_at>clock_timestamp() AND EXISTS(SELECT 1 FROM community_memberships cm WHERE cm.community_id=c.id AND cm.user_account_id IN(a.id,$2))), (SELECT min(g.expires_at) FROM consent_grants g WHERE g.owner_account_id=a.id AND g.recipient_account_id=$2 AND g.resource_type='profile' AND g.resource_id=a.id::text AND g.purpose='profile_view' AND g.actions @> ARRAY['read']::text[] AND g.revoked_at IS NULL AND g.expires_at>clock_timestamp())),
 jsonb_build_array(a.xmin::text,p.xmin::text,av.xmin::text,x.xmin::text,n.xmin::text,r.xmin::text,
 (SELECT jsonb_agg(jsonb_build_array(ag.id,ag.xmin::text) ORDER BY ag.id) FROM agents ag WHERE ag.principal_account_id=a.id),
 (SELECT jsonb_agg(jsonb_build_array(ap.agent_id,ap.xmin::text) ORDER BY ap.agent_id) FROM agent_profiles ap WHERE ap.owner_id=a.id AND ap.owner_type='PERSON'),
 (SELECT jsonb_agg(jsonb_build_array(fv.agent_id,fv.xmin::text) ORDER BY fv.agent_id) FROM agent_profile_field_visibility fv WHERE fv.owner_id=a.id),
 (SELECT jsonb_agg(jsonb_build_array(g.id,g.xmin::text) ORDER BY g.id) FROM consent_grants g WHERE g.owner_account_id=a.id AND g.recipient_account_id=$2 AND g.resource_type='profile' AND g.resource_id=a.id::text AND g.purpose='profile_view'),
 (SELECT jsonb_agg(jsonb_build_array(fl.id,fl.xmin::text) ORDER BY fl.id) FROM follows fl WHERE fl.person_account_id=a.id AND fl.follower_account_id=$2),
 (SELECT jsonb_agg(jsonb_build_array(t.id,t.xmin::text) ORDER BY t.id) FROM person_ties t WHERE t.person_a_account_id=least(a.id,$2::uuid) AND t.person_b_account_id=greatest(a.id,$2::uuid)),
 (SELECT jsonb_agg(jsonb_build_array(cr.id,cr.xmin::text) ORDER BY cr.id) FROM connection_requests cr WHERE (cr.sender_account_id=a.id AND cr.recipient_account_id=$2) OR(cr.sender_account_id=$2 AND cr.recipient_account_id=a.id)),
 (SELECT jsonb_agg(jsonb_build_array(cc.owner_account_id,cc.person_account_id,cc.xmin::text,cc.native_revision) ORDER BY cc.owner_account_id,cc.person_account_id) FROM person_close_connections cc WHERE cc.owner_account_id=a.id AND cc.person_account_id=$2),
 (SELECT jsonb_agg(jsonb_build_array(cm.id,cm.xmin::text,c.id,c.xmin::text) ORDER BY cm.id) FROM community_memberships cm JOIN communities c ON c.id=cm.community_id WHERE cm.user_account_id IN(a.id,$2)))::text
 FROM moments m JOIN accounts a ON a.id=m.author_account_id
 LEFT JOIN user_profiles p ON p.account_id=a.id LEFT JOIN person_avatars av ON av.account_id=a.id
 LEFT JOIN legacy_native_adaptation_entities x ON x.kind='moment' AND x.target_id=m.id::text AND x.owner_account_id=a.id
 LEFT JOIN legacy_native_adaptation_runs r ON r.run_id=x.run_id
 LEFT JOIN legacy_note_archives n ON n.run_id=x.run_id AND n.source_note_id::text=x.source_key AND n.source_sha256=x.source_sha256 AND n.owner_account_id=a.id AND NOT n.deleted
 WHERE m.id=ANY($1::uuid[]) ORDER BY m.id`, v.order, f.a.ID)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var id, source, stamp string
		var author content.MapContentAuthor
		var published, deadline *time.Time
		if rows.Scan(&id, &author.AccountID, &author.DisplayName, &author.Avatar, &source, &published, &deadline, &stamp) != nil {
			return nil, content.ErrUnavailable
		}
		m := v.moments[id]
		if m == nil || author.AccountID != m.owner || !author.Valid() {
			return nil, content.ErrUnavailable
		}
		m.v.Author, m.v.CreatedAtSource, m.v.PublishedAt = &author, source, published
		shrinkMapLease(&v.until, deadline)
		facts = append(facts, []any{id, author, source, published, stamp})
	}
	if rows.Err() != nil {
		return nil, content.ErrUnavailable
	}
	for _, m := range v.moments {
		if m.v.Author == nil {
			return nil, content.ErrUnavailable
		}
	}
	rows.Close()
	original, e := readMapContentOriginalAuthors(ctx, f, v)
	if e != nil {
		return nil, e
	}
	facts = append(facts, original...)
	return facts, nil
}

// These rows are immutable copies of the original source user attribution.
// Only an already-authorized original public moment may disclose them; current
// mutable private profile names/avatars are never used for this projection.
func readMapContentOriginalAuthors(ctx context.Context, f humanWriteFrame, v *mapContentSnapshot) ([]any, error) {
	facts := []any{}
	if len(v.order) == 0 {
		return facts, nil
	}
	rows, e := f.tx.Query(ctx, `SELECT af.moment_id::text,a.owner_account_id::text,a.display_name,a.source_version,coalesce(a.source_media_id::text,''),coalesce(a.thumbnail_sha256,''),coalesce(octet_length(a.thumbnail),0),af.source_created_at,
 jsonb_build_array(af.xmin::text,a.xmin::text,r.xmin::text,a.source_user_sha256,a.source_media_sha256,a.input_sha256,r.projection_sha256)::text
 FROM legacy_moment_author_facts af JOIN legacy_moment_author_snapshots a USING(run_id,source_user_id) JOIN legacy_moment_author_runs r USING(run_id)
 JOIN legacy_native_adaptation_entities x ON x.kind='moment'AND x.target_id=af.moment_id::text AND x.run_id=af.run_id AND x.source_key=af.source_note_id::text AND x.source_sha256=af.source_note_sha256 AND x.owner_account_id=a.owner_account_id
 JOIN legacy_note_archives n ON n.run_id=x.run_id AND n.source_note_id=af.source_note_id AND n.source_sha256=af.source_note_sha256 AND n.owner_account_id=a.owner_account_id AND NOT n.deleted AND n.source_visibility='public'AND n.source_moderation_status='ok'
 WHERE af.moment_id=ANY($1::uuid[])ORDER BY af.moment_id`, v.order)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var id, owner, name, media, sha, stamp string
		var version int64
		var size int
		var created *time.Time
		if rows.Scan(&id, &owner, &name, &version, &media, &sha, &size, &created, &stamp) != nil {
			return nil, content.ErrUnavailable
		}
		m := v.moments[id]
		if m == nil || m.owner != owner {
			return nil, content.ErrUnavailable
		}
		m.v.CreatedAtSource = "UNKNOWN"
		if created != nil && m.v.CreatedAt.Equal(*created) {
			m.v.CreatedAtSource = "CIVU_ORIGINAL"
		}
		if m.v.VisibilitySource == "CIVU_PUBLIC_MOMENT" {
			m.v.Author = &content.MapContentAuthor{AccountID: owner, DisplayName: name}
			if media != "" {
				source := content.MomentControlHash([]any{"legacy_moment_author_avatar_v1", m.stamp, stamp, sha})
				m.v.Author.Avatar = &av.Metadata{AccountID: owner, Version: version, ThumbnailPath: content.MapContentAuthorAvatarPath(id, media, source), Width: 128, Height: 128, ByteCount: size, SHA256: sha}
			}
			if !m.v.Author.ValidForMoment(id) {
				return nil, content.ErrUnavailable
			}
		}
		facts = append(facts, []any{id, stamp, m.v.Author, m.v.CreatedAtSource})
	}
	if rows.Err() != nil {
		return nil, content.ErrUnavailable
	}
	return facts, nil
}
func (s *Store) readHumanMapContentAuthorBinary(ctx context.Context, d [32]byte, a identity.Actor, target, id, source string, loadBytes bool) (content.MapContentBinary, error) {
	r := content.MapContentBinary{}
	if !identity.HumanControlUUID(target) || !identity.HumanControlUUID(id) || !content.MomentControlHex(source) {
		return r, content.ErrInvalid
	}
	ctx, f, e := s.beginMapContent(ctx, d, a)
	if e != nil {
		return r, e
	}
	defer f.tx.Rollback(context.Background())
	filter, e := mapContentTargetFilter(ctx, f, "moments", target)
	if e != nil {
		return r, e
	}
	v, e := readMapContentSnapshot(ctx, f, filter)
	if e != nil {
		return r, e
	}
	m := v.moments[target]
	if m == nil || m.v.VisibilitySource != "CIVU_PUBLIC_MOMENT" || m.v.Author == nil || m.v.Author.Avatar == nil || m.v.Author.Avatar.ThumbnailPath != content.MapContentAuthorAvatarPath(target, id, source) {
		return r, content.ErrNotFound
	}
	metadata := m.v.Author.Avatar
	r.MIMEType = "image/jpeg"
	r.SHA256 = metadata.SHA256
	r.ValidUntil = v.until
	if loadBytes {
		e = f.tx.QueryRow(ctx, `SELECT a.thumbnail FROM legacy_moment_author_facts f JOIN legacy_moment_author_snapshots a USING(run_id,source_user_id) WHERE f.moment_id=$1 AND a.source_media_id=$2 AND a.owner_account_id=$3 AND a.thumbnail_sha256=$4`, target, id, m.owner, r.SHA256).Scan(&r.Bytes)
		if e != nil || len(r.Bytes) != metadata.ByteCount || len(r.Bytes) > av.MaxThumbnailBytes {
			return content.MapContentBinary{}, content.ErrUnavailable
		}
	}
	if e = s.finishMapContent(ctx, f, v, r.ValidUntil); e != nil {
		clear(r.Bytes)
		return content.MapContentBinary{}, e
	}
	r.ResponseProof = &mapContentBinaryProof{s: s, d: d, a: a, targetType: "moment-authors", target: target, id: id, source: source, mimeType: r.MIMEType, sha256: r.SHA256, until: r.ValidUntil}
	return r, nil
}
