package postgres

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	_ "image/png"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

type mapContentAsset struct {
	media                    content.MapContentMedia
	image                    content.MapContentImage
	kind, blobID, mid, place string
	until                    *time.Time
}

func mapImageDimensions(header []byte) (int, int, error) {
	defer clear(header)
	c, _, e := image.DecodeConfig(bytes.NewReader(header))
	if e != nil || c.Width < 1 || c.Height < 1 || c.Width > 8192 || c.Height > 8192 || int64(c.Width)*int64(c.Height) > 48*1024*1024 {
		return 0, 0, content.ErrUnavailable
	}
	return c.Width, c.Height, nil
}
func mapImagePath(place, id, source string) string {
	return "/v1/me/map-content/places/" + place + "/images/" + id + "/content?sourceVersion=" + source
}
func mapMediaPath(mid, id, source string) string {
	return "/v1/me/map-content/moments/" + mid + "/media/" + id + "/content?sourceVersion=" + source
}

func (s *Store) mapContentMomentMediaTx(ctx context.Context, f humanWriteFrame, v *mapContentSnapshot, m *mapContentMoment) ([]mapContentAsset, error) {
	if assets, ok := v.media[m.v.ID]; ok {
		return assets, nil
	}
	assets, e := s.readMapContentMomentMediaTx(ctx, f, v, m)
	if e == nil {
		if v.media == nil {
			v.media = map[string][]mapContentAsset{}
		}
		v.media[m.v.ID] = assets
	}
	return assets, e
}

func (s *Store) readMapContentMomentMediaTx(ctx context.Context, f humanWriteFrame, v *mapContentSnapshot, m *mapContentMoment) ([]mapContentAsset, error) {
	out := []mapContentAsset{}
	if m.v.VisibilitySource == "BIRDTIE_PUBLIC_MOMENT" {
		set, e := s.mediaSetTx(ctx, f.tx, mm.Access{AccountID: f.a.ID, AccountType: f.a.AccountType, SessionDigest: f.d}, m.v.ID, false)
		if e != nil {
			return nil, content.ErrUnavailable
		}
		if set.Status == "READY" {
			for _, a := range set.Attachments {
				source := content.MomentControlHash([]any{historyReader(f), m.stamp, set.SourceVersion, a.MediaID, a.MediaVersion})
				x := mapContentAsset{kind: "public-image", blobID: a.MediaID, mid: m.v.ID, until: &set.ValidUntil}
				x.media = content.MapContentMedia{Kind: "IMAGE", ID: a.MediaID, Revision: a.MediaVersion, MIMEType: "image/jpeg", Width: a.Width, Height: a.Height, ByteCount: a.DisplayBytes, SHA256: a.DisplaySHA, SourceVersion: source, ContentPath: mapMediaPath(m.v.ID, a.MediaID, source)}
				x.image = content.MapContentImage{Kind: "MOMENT_IMAGE", ID: a.MediaID, Revision: a.MediaVersion, VisibilitySource: m.v.VisibilitySource, MIMEType: "image/jpeg", Width: a.Width, Height: a.Height, ByteCount: a.ThumbnailBytes, SHA256: a.ThumbnailSHA, SourceVersion: content.MomentControlHash([]any{source, "thumbnail"})}
				var thumbnailHeader []byte
				if e = f.tx.QueryRow(ctx, `SELECT substring(thumbnail FROM 1 FOR 65536) FROM public_moment_media_attachments WHERE moment_id=$1 AND set_version=$2 AND media_id=$3 AND retention_until>clock_timestamp()`, m.v.ID, set.SetVersion, a.MediaID).Scan(&thumbnailHeader); e != nil {
					return nil, content.ErrUnavailable
				}
				x.image.Width, x.image.Height, e = mapImageDimensions(thumbnailHeader)
				if e != nil {
					return nil, e
				}
				if a.Kind == "VIDEO" && a.Video != nil {
					c := a.Video
					d := c.DurationMS
					video, audio := "h264", c.AudioCodec
					x.kind = "public-video"
					x.media.Kind = "VIDEO"
					x.media.MIMEType = "video/mp4"
					x.media.ByteCount = c.Bytes
					x.media.SHA256 = c.SHA256
					x.media.DurationMs = &d
					x.media.VideoCodec = &video
					x.media.AudioCodec = &audio
					x.image.Kind = "MOMENT_VIDEO_POSTER"
				}
				x.image.ContentPath = mapMediaPath(m.v.ID, a.MediaID, x.image.SourceVersion) + "&variant=poster"
				if x.media.Kind == "VIDEO" {
					poster := x.image
					x.media.Poster = &poster
				}
				out = append(out, x)
			}
			return out, nil
		}
		return out, nil
	}
	// Native ready-private images remain private. Foreign reads additionally
	// require this exact untouched original public Moment, never its URL or a
	// material's private purpose as a substitute for source visibility.
	rows, e := f.tx.Query(ctx, `SELECT i.id::text,i.revision,i.mime_type,octet_length(i.derivative),i.derivative_sha256,substring(i.derivative FROM 1 FOR 65536),i.retain_until,jsonb_build_array(i.xmin::text,a.xmin::text)::text
 FROM private_moment_images i JOIN media_assets a ON a.id=i.id AND a.owner_account_id=i.owner_account_id AND a.state='ready_private' AND a.sha256_hex=i.derivative_sha256 AND a.byte_size=octet_length(i.derivative)
 WHERE i.moment_id=$1 AND i.owner_account_id=$2 AND i.status='ready_private' AND i.derivative IS NOT NULL AND i.retain_until>clock_timestamp()
 AND($3 OR EXISTS(SELECT 1 FROM(SELECT run_id,source_note_id,owner_account_id,moment_id,image_id FROM legacy_private_media_import_items UNION ALL SELECT run_id,source_note_id,owner_account_id,moment_id,image_id FROM legacy_private_image_recovery_items) imp JOIN legacy_native_adaptation_entities n ON n.run_id=imp.run_id AND n.kind='moment' AND n.source_key=imp.source_note_id::text AND n.target_id=imp.moment_id::text AND n.owner_account_id=imp.owner_account_id WHERE imp.image_id=i.id AND imp.moment_id=i.moment_id AND imp.owner_account_id=i.owner_account_id))
 ORDER BY i.created_at,i.id LIMIT 201`, m.v.ID, m.owner, m.owner == f.a.ID)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	for rows.Next() {
		x := mapContentAsset{kind: "private-image", mid: m.v.ID}
		var header []byte
		var stamp string
		if e = rows.Scan(&x.media.ID, &x.media.Revision, &x.media.MIMEType, &x.media.ByteCount, &x.media.SHA256, &header, &x.until, &stamp); e != nil {
			break
		}
		x.media.Width, x.media.Height, e = mapImageDimensions(header)
		if e != nil {
			break
		}
		x.blobID = x.media.ID
		x.media.Kind = "IMAGE"
		x.media.SourceVersion = content.MomentControlHash([]any{historyReader(f), m.stamp, stamp, x.media.ID, x.media.Revision, x.media.SHA256})
		x.media.ContentPath = mapMediaPath(m.v.ID, x.media.ID, x.media.SourceVersion)
		x.image = content.MapContentImage{Kind: "MOMENT_IMAGE", ID: x.media.ID, Revision: x.media.Revision, VisibilitySource: m.v.VisibilitySource, MIMEType: x.media.MIMEType, Width: x.media.Width, Height: x.media.Height, ByteCount: x.media.ByteCount, SHA256: x.media.SHA256, SourceVersion: x.media.SourceVersion, ContentPath: x.media.ContentPath}
		out = append(out, x)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || len(out) > 200 {
		return nil, content.ErrUnavailable
	}
	rows, e = f.tx.Query(ctx, `SELECT v.id::text,v.revision,v.width,v.height,v.content_size,v.content_sha256,v.duration_ms,v.audio_codec,v.poster_size,v.poster_sha256,v.retain_until,jsonb_build_array(v.xmin::text,a.xmin::text,imp.xmin::text)::text
 FROM private_moment_videos v JOIN media_assets a ON a.id=v.id AND a.owner_account_id=v.owner_account_id AND a.state='ready_private' AND a.sha256_hex=v.content_sha256 AND a.byte_size=v.content_size JOIN legacy_private_video_import_items imp ON imp.video_id=v.id AND imp.moment_id=v.moment_id AND imp.owner_account_id=v.owner_account_id AND imp.run_id=v.run_id
 WHERE v.moment_id=$1 AND v.owner_account_id=$2 AND v.status='ready_private' AND v.content IS NOT NULL AND v.poster IS NOT NULL AND v.retain_until>clock_timestamp() ORDER BY v.source_position,v.id LIMIT 201`, m.v.ID, m.owner)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	for rows.Next() {
		x := mapContentAsset{kind: "private-video", mid: m.v.ID}
		var duration int64
		var audio, stamp string
		var posterSize int
		var posterSHA string
		if e = rows.Scan(&x.media.ID, &x.media.Revision, &x.media.Width, &x.media.Height, &x.media.ByteCount, &x.media.SHA256, &duration, &audio, &posterSize, &posterSHA, &x.until, &stamp); e != nil {
			break
		}
		x.blobID = x.media.ID
		x.media.Kind = "VIDEO"
		x.media.MIMEType = "video/mp4"
		video := "h264"
		x.media.DurationMs = &duration
		x.media.VideoCodec = &video
		x.media.AudioCodec = &audio
		x.media.SourceVersion = content.MomentControlHash([]any{historyReader(f), m.stamp, stamp, x.media.ID, x.media.Revision, x.media.SHA256})
		x.media.ContentPath = mapMediaPath(m.v.ID, x.media.ID, x.media.SourceVersion)
		x.image = content.MapContentImage{Kind: "MOMENT_VIDEO_POSTER", ID: x.media.ID, Revision: x.media.Revision, VisibilitySource: m.v.VisibilitySource, MIMEType: "image/jpeg", Width: x.media.Width, Height: x.media.Height, ByteCount: posterSize, SHA256: posterSHA, SourceVersion: content.MomentControlHash([]any{x.media.SourceVersion, "poster", posterSHA})}
		x.image.ContentPath = mapMediaPath(m.v.ID, x.media.ID, x.image.SourceVersion) + "&variant=poster"
		poster := x.image
		x.media.Poster = &poster
		out = append(out, x)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || len(out) > 200 {
		return nil, content.ErrUnavailable
	}
	return out, nil
}

func (s *Store) mapContentReferenceAssetsTx(ctx context.Context, f humanWriteFrame, v *mapContentSnapshot, pid string) ([]mapContentAsset, error) {
	if assets, ok := v.references[pid]; ok {
		return assets, nil
	}
	if e := s.loadMapContentReferenceAssetsTx(ctx, f, v, []string{pid}); e != nil {
		return nil, e
	}
	return v.references[pid], nil
}

func (s *Store) loadMapContentReferenceAssetsTx(ctx context.Context, f humanWriteFrame, v *mapContentSnapshot, pids []string) error {
	if v.references == nil {
		v.references = map[string][]mapContentAsset{}
	}
	missing := []string{}
	for _, pid := range pids {
		if _, ok := v.references[pid]; !ok {
			missing = append(missing, pid)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if len(missing) > 500 {
		return content.ErrUnavailable
	}
	loaded := map[string][]mapContentAsset{}
	for _, pid := range missing {
		loaded[pid] = []mapContentAsset{}
	}
	rows, e := f.tx.Query(ctx, `WITH bounded AS(SELECT r.target_id::text,r.id::text,l.revision,r.owner_account_id::text,r.review_status,r.rights_basis,r.source_note_id::text,m.mime_type,m.width,m.height,m.byte_size,m.sha256,m.id::text,coalesce(m.backing_image_id::text,''),coalesce(note_mapping.target_id,''),
 least(backing.retain_until,p.expires_at),jsonb_build_array(r.xmin::text,l.xmin::text,m.xmin::text,af.xmin::text,ir.xmin::text,original.xmin::text,u.xmin::text,p.xmin::text,backing.xmin::text,bm.xmin::text,asset.xmin::text)::text,
 row_number() OVER(PARTITION BY r.target_id ORDER BY r.source_is_cover DESC,r.source_position,r.id) candidate_position
 FROM legacy_reference_media_relations r JOIN private_reference_image_links l ON l.id=r.id AND l.status='ACTIVE'
 JOIN private_reference_image_materials m ON m.id=l.material_id AND m.owner_account_id=r.owner_account_id AND m.status='READY_PRIVATE'
 JOIN legacy_reference_media_assets af ON af.run_id=r.run_id AND af.source_media_id=r.source_media_id AND af.owner_account_id=r.owner_account_id AND af.material_id=m.id AND NOT af.system_archive
 JOIN legacy_reference_media_runs ir ON ir.run_id=r.run_id JOIN legacy_import_runs original ON original.run_id=ir.run_id AND original.source_file_sha256=ir.source_file_sha256 AND original.manifest_file_sha256=ir.manifest_file_sha256 AND original.snapshot_sha256=ir.snapshot_sha256
 JOIN legacy_entity_mappings u ON u.run_id=r.run_id AND u.entity_kind='user' AND u.source_id=r.source_owner_id AND u.target_id=r.owner_account_id AND u.owner_account_id=r.owner_account_id
 JOIN accounts contributor ON contributor.id=r.owner_account_id AND contributor.account_type='person'
 LEFT JOIN legacy_native_adaptation_entities note_mapping ON note_mapping.run_id=r.run_id AND note_mapping.kind='moment' AND note_mapping.source_key=r.source_note_id::text AND note_mapping.owner_account_id=r.owner_account_id
 JOIN places p ON p.id=r.target_id JOIN legacy_map_place_facts pf ON pf.place_id=p.id AND pf.run_id=r.run_id AND pf.source_place_id=r.source_parent_id AND pf.source_sha256=r.source_parent_sha256
 LEFT JOIN private_moment_images backing ON backing.id=m.backing_image_id AND backing.owner_account_id=m.owner_account_id
 LEFT JOIN moments bm ON bm.id=backing.moment_id AND bm.author_account_id=backing.owner_account_id
 LEFT JOIN media_assets asset ON asset.id=backing.id AND asset.owner_account_id=backing.owner_account_id
 WHERE r.target_type='PLACE' AND r.target_id=ANY($1::uuid[]) AND NOT r.tombstone AND r.review_status<>'removed'
 AND(r.owner_account_id=$2 OR(u.source_status='active' AND(contributor.status='active' OR(contributor.status='suspended' AND contributor.xmin=original.xmin))))
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE(bl.blocker_account_id=$2 AND bl.blocked_account_id=r.owner_account_id)OR(bl.blocker_account_id=r.owner_account_id AND bl.blocked_account_id=$2))
 AND((m.backing_image_id IS NULL AND octet_length(m.content)=m.byte_size)
 OR(m.backing_image_id IS NOT NULL AND backing.status='ready_private' AND backing.retain_until>clock_timestamp() AND asset.state='ready_private' AND asset.sha256_hex=m.sha256 AND asset.byte_size=m.byte_size AND backing.derivative_sha256=m.sha256 AND bm.status='draft' AND bm.visibility='private' AND octet_length(backing.derivative)=m.byte_size))
 ) SELECT * FROM bounded WHERE candidate_position<=1001 ORDER BY 1,candidate_position`, missing, f.a.ID)
	if e != nil {
		return content.ErrUnavailable
	}
	for rows.Next() {
		x := mapContentAsset{kind: "reference"}
		var owner, review, rights, backing, native, stamp string
		var note *string
		var position int64
		if e = rows.Scan(&x.place, &x.image.ID, &x.image.Revision, &owner, &review, &rights, &note, &x.image.MIMEType, &x.image.Width, &x.image.Height, &x.image.ByteCount, &x.image.SHA256, &x.blobID, &backing, &native, &x.until, &stamp, &position); e != nil {
			break
		}
		pid := x.place
		scope := ""
		if owner == f.a.ID {
			scope = "SELF_PRIVATE"
		} else if review == "approved" && (rights == "user_owned" || rights == "licensed" || rights == "official" || rights == "provider_terms") && v.places[pid] != nil && v.places[pid].catalog {
			if note == nil {
				scope = "CIVU_PUBLIC_CATALOG"
			} else {
				m := v.moments[native]
				if m != nil && (m.v.VisibilitySource == "CIVU_PUBLIC_MOMENT" || m.v.VisibilitySource == "BIRDTIE_PUBLIC_MOMENT") {
					scope = "CIVU_PUBLIC_MOMENT"
				}
			}
		}
		if scope == "" {
			continue
		}
		x.image.Kind = "REFERENCE_IMAGE"
		x.image.VisibilitySource = scope
		x.image.SourceVersion = content.MomentControlHash([]any{historyReader(f), pid, scope, stamp, x.image.ID, x.image.Revision, x.image.SHA256})
		x.image.ContentPath = mapImagePath(pid, x.image.ID, x.image.SourceVersion)
		if x.image.Valid() {
			loaded[pid] = append(loaded[pid], x)
			if len(loaded[pid]) > 1000 {
				e = content.ErrUnavailable
				break
			}
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return content.ErrUnavailable
	}
	for pid, assets := range loaded {
		v.references[pid] = assets
	}
	return nil
}

func (s *Store) mapContentCoverTx(ctx context.Context, f humanWriteFrame, v *mapContentSnapshot, pid string) (*content.MapContentImage, error) {
	images, e := s.mapContentReferenceAssetsTx(ctx, f, v, pid)
	if e != nil {
		return nil, e
	}
	if len(images) > 0 {
		shrinkMapLease(&v.until, images[0].until)
		x := images[0].image
		return &x, nil
	}
	for _, mid := range v.order {
		m := v.moments[mid]
		if !containsMapString(m.places, pid) {
			continue
		}
		assets, e := s.mapContentMomentMediaTx(ctx, f, v, m)
		if e != nil {
			return nil, e
		}
		if len(assets) > 0 {
			shrinkMapLease(&v.until, assets[0].until)
			x := assets[0].image
			x.SourceVersion = content.MomentControlHash([]any{x.SourceVersion, pid, "cover"})
			x.ContentPath = mapImagePath(pid, x.ID, x.SourceVersion)
			return &x, nil
		}
	}
	return nil, nil
}

type mapContentBinaryProof struct {
	s                                       *Store
	d                                       [32]byte
	a                                       identity.Actor
	targetType, target, id, source, variant string
	mimeType, sha256                        string
	until                                   time.Time
}

func (p *mapContentBinaryProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.until.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	// Rebuild the same current authority and exact selector, without fetching
	// the already hash-checked large bytea a second time merely to discard it.
	b, e := p.s.readHumanMapContentBinary(ctx, p.d, p.a, p.targetType, p.target, p.id, p.source, p.variant, false)
	if e != nil {
		return e
	}
	if b.ValidUntil.Before(p.until) || b.MIMEType != p.mimeType || b.SHA256 != p.sha256 {
		return identity.ErrControlChanged
	}
	return nil
}

func (s *Store) ReadHumanMapContentBinary(ctx context.Context, d [32]byte, a identity.Actor, targetType, target, id, source, variant string) (content.MapContentBinary, error) {
	return s.readHumanMapContentBinary(ctx, d, a, targetType, target, id, source, variant, true)
}

func (s *Store) readHumanMapContentBinary(ctx context.Context, d [32]byte, a identity.Actor, targetType, target, id, source, variant string, loadBytes bool) (content.MapContentBinary, error) {
	if targetType == "moment-authors" && variant == "" {
		return s.readHumanMapContentAuthorBinary(ctx, d, a, target, id, source, loadBytes)
	}
	r := content.MapContentBinary{}
	if (targetType != "places" && targetType != "moments") || !identity.HumanControlUUID(target) || !identity.HumanControlUUID(id) || !content.MomentControlHex(source) || (variant != "" && variant != "poster" && variant != "marker" && variant != "display") || targetType == "places" && variant != "" && variant != "marker" {
		return r, content.ErrInvalid
	}
	ctx, f, e := s.beginMapContent(ctx, d, a)
	if e != nil {
		return r, e
	}
	defer f.tx.Rollback(context.Background())
	filter, e := mapContentTargetFilter(ctx, f, targetType, target)
	if e != nil {
		return r, e
	}
	v, e := readMapContentSnapshot(ctx, f, filter)
	if e != nil {
		return r, e
	}
	r.ValidUntil = v.until
	var chosen *mapContentAsset
	marker := variant == "marker"
	display := variant == "display"
	poster := variant == "poster" || marker
	if targetType == "moments" {
		m := v.moments[target]
		if m == nil {
			return r, content.ErrNotFound
		}
		assets, e := s.mapContentMomentMediaTx(ctx, f, &v, m)
		if e != nil {
			return r, e
		}
		for _, x := range assets {
			want := x.media.SourceVersion
			if display {
				if x.kind != "private-image" {
					continue
				}
				want = mapDisplayVersion(want)
			}
			if poster {
				want = x.image.SourceVersion
			}
			if marker {
				want = mapMarkerVersion(want)
			}
			if x.media.ID == id && want == source {
				xx := x
				chosen = &xx
				break
			}
		}
	} else {
		p := v.places[target]
		if p == nil || len(p.pin.VisibilitySources) == 0 {
			return r, content.ErrNotFound
		}
		shrinkMapLease(&r.ValidUntil, p.until)
		if p.catalog || p.published {
			shrinkMapLease(&r.ValidUntil, p.publicUntil)
		}
		refs, e := s.mapContentReferenceAssetsTx(ctx, f, &v, target)
		if e != nil {
			return r, e
		}
		for _, x := range refs {
			want := x.image.SourceVersion
			if marker {
				want = mapMarkerVersion(want)
			}
			if x.image.ID == id && want == source {
				xx := x
				chosen = &xx
				break
			}
		}
		if chosen == nil {
			for _, mid := range v.order {
				m := v.moments[mid]
				if !containsMapString(m.places, target) {
					continue
				}
				assets, e := s.mapContentMomentMediaTx(ctx, f, &v, m)
				if e != nil {
					return r, e
				}
				for _, x := range assets {
					want := content.MomentControlHash([]any{x.image.SourceVersion, target, "cover"})
					if marker {
						want = mapMarkerVersion(want)
					}
					if x.image.ID == id && want == source {
						xx := x
						chosen = &xx
						poster = true
						break
					}
				}
				if chosen != nil {
					break
				}
			}
		}
	}
	if chosen == nil {
		return r, content.ErrNotFound
	}
	x := *chosen
	shrinkMapLease(&r.ValidUntil, x.until)
	r.MIMEType, r.SHA256 = x.media.MIMEType, x.media.SHA256
	if targetType == "places" || poster || x.kind == "reference" {
		r.MIMEType, r.SHA256 = x.image.MIMEType, x.image.SHA256
	}
	if marker || display {
		var thumb mapMarkerThumbnail
		var err error
		if display {
			thumb, err = s.mapDisplayBytesTx(ctx, f, x)
		} else {
			thumb, err = s.mapMarkerBytesTx(ctx, f, x, source)
		}
		if err != nil {
			return r, err
		}
		r.MIMEType = "image/jpeg"
		r.SHA256 = thumb.sha
		if e = s.finishMapContent(ctx, f, v, r.ValidUntil); e != nil {
			return r, e
		}
		if loadBytes {
			r.Bytes = append([]byte(nil), thumb.bytes...)
			r.ResponseProof = &mapContentBinaryProof{s: s, d: d, a: a, targetType: targetType, target: target, id: id, source: source, variant: variant, mimeType: r.MIMEType, sha256: r.SHA256, until: r.ValidUntil}
		}
		return r, nil
	}
	if !loadBytes {
		if e = s.finishMapContent(ctx, f, v, r.ValidUntil); e != nil {
			return r, e
		}
		return r, nil
	}
	var raw []byte
	switch x.kind {
	case "reference":
		e = f.tx.QueryRow(ctx, `SELECT coalesce(m.content,i.derivative) FROM private_reference_image_materials m LEFT JOIN private_moment_images i ON i.id=m.backing_image_id WHERE m.id=$1 AND m.status='READY_PRIVATE'`, x.blobID).Scan(&raw)
		r.MIMEType, r.SHA256 = x.image.MIMEType, x.image.SHA256
	case "private-image":
		e = f.tx.QueryRow(ctx, `SELECT derivative FROM private_moment_images WHERE id=$1 AND status='ready_private' AND retain_until>clock_timestamp()`, x.blobID).Scan(&raw)
		r.MIMEType, r.SHA256 = x.media.MIMEType, x.media.SHA256
	case "private-video":
		if poster {
			e = f.tx.QueryRow(ctx, `SELECT poster FROM private_moment_videos WHERE id=$1 AND status='ready_private' AND retain_until>clock_timestamp()`, x.blobID).Scan(&raw)
			r.MIMEType, r.SHA256 = x.image.MIMEType, x.image.SHA256
		} else {
			e = f.tx.QueryRow(ctx, `SELECT content FROM private_moment_videos WHERE id=$1 AND status='ready_private' AND retain_until>clock_timestamp()`, x.blobID).Scan(&raw)
			r.MIMEType, r.SHA256 = x.media.MIMEType, x.media.SHA256
		}
	case "public-image", "public-video":
		if x.kind == "public-video" && !poster {
			e = f.tx.QueryRow(ctx, `SELECT p.content FROM public_moment_media_video_payloads p JOIN public_moment_media_sets s ON s.moment_id=p.moment_id AND s.version=p.set_version AND s.status='READY' WHERE p.moment_id=$1 AND p.media_id=$2 AND p.content IS NOT NULL`, x.mid, x.blobID).Scan(&raw)
			r.MIMEType, r.SHA256 = x.media.MIMEType, x.media.SHA256
		} else {
			column := "display"
			r.MIMEType, r.SHA256 = x.media.MIMEType, x.media.SHA256
			if poster {
				column = "thumbnail"
				r.MIMEType, r.SHA256 = x.image.MIMEType, x.image.SHA256
			}
			e = f.tx.QueryRow(ctx, `SELECT a.`+column+` FROM public_moment_media_attachments a JOIN public_moment_media_sets s ON s.moment_id=a.moment_id AND s.version=a.set_version AND s.status='READY' WHERE a.moment_id=$1 AND a.media_id=$2 AND a.retention_until>clock_timestamp()`, x.mid, x.blobID).Scan(&raw)
		}
	default:
		return r, content.ErrNotFound
	}
	expectedBytes := x.media.ByteCount
	if targetType == "places" || poster || x.kind == "reference" {
		expectedBytes = x.image.ByteCount
	}
	if e != nil || len(raw) == 0 || len(raw) != expectedBytes || len(raw) > 64*1024*1024 || mm.DigestBytes(raw) != r.SHA256 {
		clear(raw)
		return r, content.ErrUnavailable
	}
	if e = s.finishMapContent(ctx, f, v, r.ValidUntil); e != nil {
		clear(raw)
		return r, e
	}
	r.Bytes = raw
	r.ResponseProof = &mapContentBinaryProof{s: s, d: d, a: a, targetType: targetType, target: target, id: id, source: source, variant: variant, mimeType: r.MIMEType, sha256: r.SHA256, until: r.ValidUntil}
	return r, nil
}

// Small derivatives are cached in RAM only. The cache never answers an HTTP
// request before that request rebuilds its current content/media authority.
// Stable source versions isolate readers without storing a bearer token.
type mapMarkerThumbnail struct {
	bytes         []byte
	width, height int
	sha           string
	used          time.Time
}

var mapMarkerCache = struct {
	sync.Mutex
	items map[string]mapMarkerThumbnail
	bytes int
}{items: map[string]mapMarkerThumbnail{}}
var mapMarkerDecodes = make(chan struct{}, 2)

func mapMarkerVersion(source string) string {
	return content.MomentControlHash([]any{source, "marker160-v1"})
}
func cachedMapMarker(key string) (mapMarkerThumbnail, bool) {
	mapMarkerCache.Lock()
	defer mapMarkerCache.Unlock()
	v, ok := mapMarkerCache.items[key]
	if ok {
		v.used = time.Now()
		mapMarkerCache.items[key] = v
	}
	return v, ok
}
func saveMapMarker(key string, v mapMarkerThumbnail) {
	mapMarkerCache.Lock()
	defer mapMarkerCache.Unlock()
	if old, ok := mapMarkerCache.items[key]; ok {
		mapMarkerCache.bytes -= len(old.bytes)
	}
	v.used = time.Now()
	mapMarkerCache.items[key] = v
	mapMarkerCache.bytes += len(v.bytes)
	for len(mapMarkerCache.items) > 96 || mapMarkerCache.bytes > 8*1024*1024 {
		oldest := ""
		var at time.Time
		for k, x := range mapMarkerCache.items {
			if oldest == "" || x.used.Before(at) {
				oldest = k
				at = x.used
			}
		}
		mapMarkerCache.bytes -= len(mapMarkerCache.items[oldest].bytes)
		delete(mapMarkerCache.items, oldest)
	}
}
func (s *Store) mapMarkerBytesTx(ctx context.Context, f humanWriteFrame, x mapContentAsset, key string) (mapMarkerThumbnail, error) {
	if cached, ok := cachedMapMarker(key); ok {
		return cached, nil
	}
	select {
	case mapMarkerDecodes <- struct{}{}:
	case <-ctx.Done():
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	defer func() { <-mapMarkerDecodes }()
	if cached, ok := cachedMapMarker(key); ok {
		return cached, nil
	}
	var raw []byte
	var e error
	switch x.kind {
	case "reference":
		e = f.tx.QueryRow(ctx, `SELECT coalesce(m.content,i.derivative) FROM private_reference_image_materials m LEFT JOIN private_moment_images i ON i.id=m.backing_image_id WHERE m.id=$1 AND m.status='READY_PRIVATE'`, x.blobID).Scan(&raw)
	case "private-image":
		e = f.tx.QueryRow(ctx, `SELECT derivative FROM private_moment_images WHERE id=$1 AND status='ready_private' AND retain_until>clock_timestamp()`, x.blobID).Scan(&raw)
	case "private-video":
		e = f.tx.QueryRow(ctx, `SELECT poster FROM private_moment_videos WHERE id=$1 AND status='ready_private' AND retain_until>clock_timestamp()`, x.blobID).Scan(&raw)
	case "public-image", "public-video":
		e = f.tx.QueryRow(ctx, `SELECT a.thumbnail FROM public_moment_media_attachments a JOIN public_moment_media_sets s ON s.moment_id=a.moment_id AND s.version=a.set_version AND s.status='READY' WHERE a.moment_id=$1 AND a.media_id=$2 AND a.retention_until>clock_timestamp()`, x.mid, x.blobID).Scan(&raw)
	default:
		return mapMarkerThumbnail{}, content.ErrNotFound
	}
	defer clear(raw)
	if e != nil || len(raw) < 1 || len(raw) > 16*1024*1024 || mm.DigestBytes(raw) != x.image.SHA256 {
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 48*1024*1024 {
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	decoded, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	w, h := cfg.Width, cfg.Height
	if w > 160 || h > 160 {
		scale := 160.0 / float64(max(w, h))
		w = max(1, int(float64(w)*scale))
		h = max(1, int(float64(h)*scale))
	}
	small := image.NewRGBA(image.Rect(0, 0, w, h))
	bounds := decoded.Bounds()
	for y := 0; y < h; y++ {
		for xx := 0; xx < w; xx++ {
			small.Set(xx, y, decoded.At(bounds.Min.X+xx*cfg.Width/w, bounds.Min.Y+y*cfg.Height/h))
		}
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, small, &jpeg.Options{Quality: 76}) != nil {
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	v := mapMarkerThumbnail{bytes: out.Bytes(), width: w, height: h, sha: mm.DigestBytes(out.Bytes())}
	saveMapMarker(key, v)
	return v, nil
}
func (s *Store) mapMarkerThumbnailTx(ctx context.Context, f humanWriteFrame, v *mapContentSnapshot, original content.MapContentImage) (*content.MapContentImage, error) {
	var chosen *mapContentAsset
	u, e := url.Parse(original.ContentPath)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) != 9 {
		return nil, content.ErrUnavailable
	}
	target := parts[5]
	if parts[4] == "places" {
		for _, x := range v.references[target] {
			if x.image.ID == original.ID {
				copy := x
				chosen = &copy
				break
			}
		}
	}
	if chosen == nil {
		for _, assets := range v.media {
			for _, x := range assets {
				if x.image.ID == original.ID {
					copy := x
					chosen = &copy
					break
				}
			}
			if chosen != nil {
				break
			}
		}
	}
	if chosen == nil {
		return nil, content.ErrUnavailable
	}
	key := mapMarkerVersion(original.SourceVersion)
	thumb, e := s.mapMarkerBytesTx(ctx, f, *chosen, key)
	if e != nil {
		return nil, e
	}
	shrinkMapLease(&v.until, chosen.until)
	result := original
	result.Width = thumb.width
	result.Height = thumb.height
	result.ByteCount = len(thumb.bytes)
	result.SHA256 = thumb.sha
	result.MIMEType = "image/jpeg"
	result.SourceVersion = key
	q := u.Query()
	q.Set("sourceVersion", key)
	q.Set("variant", "marker")
	u.RawQuery = q.Encode()
	result.ContentPath = u.String()
	return &result, nil
}
