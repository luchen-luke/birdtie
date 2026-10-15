package postgres

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"reflect"
	"strings"

	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	mphoto "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
)

var _ mp.StoreWithMedia = (*Store)(nil)

// Decoration authority is separate from the base point and never crosses the
// wire. One PG stamp/MVCC statement supplies all visible places' full counts,
// source generations and approved derivatives, after native Session waits.
func mapMediaProjectionSQL() string {
	base := mapProjectionSQL(false)
	marker := " SELECT stamp.at,"
	if strings.Count(base, marker) != 1 || strings.Count(base, "payload.items,payload.truncated") != 1 {
		return ""
	}
	deadline := `least(stamp.at+interval '60 seconds',c.expires_at,payload.deadline,(SELECT deadline FROM native_actor),recent.until)`
	media := `,
 map_media_sources AS MATERIALIZED(SELECT b.id,
 birdtie_place_photo_metadata(b.id::uuid,stamp.at) photo,
 birdtie_place_photo_proof(b.id::uuid) photo_token,
 CASE WHEN ` + deadline + `>stamp.at THEN jsonb_build_object('count',recent.n,'checkedAt',stamp.at,
 'windowStart',stamp.at-interval '30 days','validUntil',` + deadline + `) ELSE NULL END recent,
 recent.token count_token
 FROM bounded b CROSS JOIN stamp CROSS JOIN city c CROSS JOIN payload
 LEFT JOIN LATERAL(SELECT count(DISTINCT m.id) n,min(least(m.published_at+interval '30 days',links.deadline,` + momentPublishableMediaDeadlineSQL("m") + `)) until,
 encode(sha256(convert_to(coalesce(jsonb_agg(jsonb_build_array(m.id,m.xmin::text,a.xmin::text,links.binding,
 ` + mapPublicAuthorSafetyGenerationSQL + `,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text) ORDER BY ep.owner_account_id,ep.target_account_id),'[]'::jsonb)
 FROM person_block_control_epochs ep WHERE $6::uuid IS NOT NULL AND
 ((ep.owner_account_id=$6 AND ep.target_account_id=a.id) OR(ep.owner_account_id=a.id AND ep.target_account_id=$6)))) ORDER BY m.id),'[]'::jsonb)::text,'UTF8')),'hex') token
 FROM moments m JOIN accounts a ON a.id=m.author_account_id
 JOIN LATERAL(SELECT min(location.deadline) deadline,string_agg(location.binding,':' ORDER BY location.id) binding
   FROM birdtie_moment_locations_public(m.id,stamp.at) location
   WHERE location.kind='PLACE' AND location.precision='place' AND location.place_id=b.id::uuid HAVING count(*)>0) links ON true
 WHERE ` + recentPublicMomentPredicate("stamp.at", "$6") + `) recent ON true
 WHERE b.kind='PLACE' AND b.rank<=30),
 map_media_payload AS MATERIALIZED(SELECT
 coalesce((SELECT jsonb_agg(item || CASE WHEN item->>'kind'='PLACE' THEN
 jsonb_build_object('placePhoto',media.photo,'recentMoments',media.recent) ELSE '{}'::jsonb END ORDER BY item->>'kind',item->>'id')
 FROM jsonb_array_elements(payload.items) item LEFT JOIN map_media_sources media ON media.id=item->>'id' AND item->>'kind'='PLACE'),'[]'::jsonb) items,
 coalesce((SELECT jsonb_object_agg(id,jsonb_build_object('photo',photo_token,'count',count_token)) FROM map_media_sources),'{}'::jsonb)::text proof
 FROM payload)
`
	base = strings.Replace(base, marker, media+marker, 1)
	base = strings.Replace(base, "payload.items,payload.truncated", "map_media_payload.items,payload.truncated", 1)
	end := "($6::uuid IS NULL OR EXISTS(SELECT 1 FROM native_actor)),NULL::jsonb"
	if strings.Count(base, end) != 1 || strings.Count(base, "FROM city c CROSS JOIN stamp CROSS JOIN payload") != 1 {
		return ""
	}
	base = strings.Replace(base, end, end+",map_media_payload.proof", 1)
	return strings.Replace(base, "FROM city c CROSS JOIN stamp CROSS JOIN payload", "FROM city c CROSS JOIN stamp CROSS JOIN payload CROSS JOIN map_media_payload", 1)
}

type mapDecorationProof struct {
	Photo       string `json:"photo"`
	Count       string `json:"count"`
	MomentPhoto string `json:"momentPhoto"`
}

func mapBaseItems(items []mp.Item) []mp.Item {
	out := append(items[:0:0], items...)
	for i := range out {
		out[i].PlacePhoto, out[i].RecentMoments = nil, nil
		out[i].MomentPhoto = nil
	}
	return out
}

func (s *Store) FinalizeMapLayers(ctx context.Context, a mp.Access, old mp.Receipt) (mp.View, error) {
	if !(old.Query.Media || old.Query.MomentMedia) || !a.Valid() || mp.ValidateViewForQuery(old.View, old.Query) != nil {
		return mp.View{}, mp.ErrInvalid
	}
	seal, err := mapProjectionSeal(a, old)
	if err != nil || !hmac.Equal([]byte(seal), []byte(old.Seal)) {
		return mp.View{}, mp.ErrChanged
	}
	current, err := s.captureMapLayers(ctx, a, old.Query)
	if err != nil {
		return mp.View{}, err
	}
	if !old.View.ValidUntil.After(current.View.ObservedAt) || old.Proof != current.Proof ||
		old.View.Truncated != current.View.Truncated || !reflect.DeepEqual(mapBaseItems(old.View.Items), mapBaseItems(current.View.Items)) {
		return mp.View{}, mp.ErrChanged
	}
	var before, after map[string]mapDecorationProof
	if json.Unmarshal([]byte(old.MediaProof), &before) != nil || json.Unmarshal([]byte(current.MediaProof), &after) != nil {
		return mp.View{}, mp.ErrUnavailable
	}
	// Retain only original, still-current decoration authority. A changed media
	// source produces a symbol/unknown summary here, not a fresh borrowed photo.
	out := mp.RetireExpiredMedia(old.View, current.View.ObservedAt)
	for i := range out.Items {
		item, fresh := &out.Items[i], current.View.Items[i]
		key := item.ID
		if old.Query.MomentMedia {
			key = item.Kind + ":" + item.ID
		}
		b, bok := before[key]
		c, cok := after[key]
		if item.PlacePhoto != nil {
			var next *pp.Metadata
			if fresh.PlacePhoto != nil {
				copy := *fresh.PlacePhoto
				next = &copy
			}
			if !bok || !cok || b.Photo != c.Photo || !pp.PreserveLease(item.PlacePhoto, next, current.View.ObservedAt) {
				item.PlacePhoto = nil
			}
		}
		if item.RecentMoments != nil && (!bok || !cok || b.Count != c.Count || fresh.RecentMoments == nil || fresh.RecentMoments.Count != item.RecentMoments.Count) {
			item.RecentMoments = nil
		}
		if item.MomentPhoto != nil {
			var next *mphoto.Metadata
			if fresh.MomentPhoto != nil {
				copy := *fresh.MomentPhoto
				next = &copy
			}
			if !bok || !cok || b.MomentPhoto != c.MomentPhoto || !mphoto.PreserveLease(item.MomentPhoto, next, current.View.ObservedAt) {
				item.MomentPhoto = nil
			}
		}
	}
	return out, nil
}

// Optional media is read with the original base SQL stamp and viewer. This
// preserves original item IDs, source versions, anchors and base proof. Author
// selection remains the current explicit public Profile source, not a new feed.
func mapMomentMediaProjectionSQL(author bool) string {
	base := mapMediaProjectionSQL()
	if author {
		base = mapPublicAuthorProjectionSQL
	}
	marker := " SELECT stamp.at,"
	if strings.Count(base, marker) != 1 {
		return ""
	}
	media := `,
 map_moment_media_sources AS MATERIALIZED(SELECT b.id,
 birdtie_public_moment_photo_metadata(b.id::uuid,$6::uuid,stamp.at) photo,
 birdtie_public_moment_photo_proof(b.id::uuid,$6::uuid) token
 FROM bounded b CROSS JOIN stamp WHERE b.kind='MOMENT' AND b.rank<=30),
 map_moment_media_payload AS MATERIALIZED(SELECT coalesce((SELECT jsonb_agg(item ||
 CASE WHEN item->>'kind'='MOMENT' THEN jsonb_build_object('momentPhoto',media.photo) ELSE '{}'::jsonb END
 ORDER BY item->>'kind',item->>'id') FROM jsonb_array_elements(BASE_ITEMS) item
 LEFT JOIN map_moment_media_sources media ON media.id=item->>'id' AND item->>'kind'='MOMENT'),'[]'::jsonb) items,
 (BASE_PROOF || coalesce((SELECT jsonb_object_agg('MOMENT:'||id,jsonb_build_object('momentPhoto',token))
	 FROM map_moment_media_sources),'{}'::jsonb))::text proof FROM BASE_FROM)
`
	items, proof, from := "map_media_payload.items", "coalesce((SELECT jsonb_object_agg('PLACE:'||key,value) FROM jsonb_each(map_media_payload.proof::jsonb)),'{}'::jsonb)", "payload CROSS JOIN map_media_payload"
	if author {
		items, proof, from = "payload.items", "'{}'::jsonb", "payload"
	}
	media = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(media, "BASE_ITEMS", items), "BASE_PROOF", proof), "BASE_FROM", from)
	base = strings.Replace(base, marker, media+marker, 1)
	base = strings.Replace(base, items+",payload.truncated", "map_moment_media_payload.items,payload.truncated", 1)
	if author {
		end := "($6::uuid IS NULL OR EXISTS(SELECT 1 FROM native_actor)),NULL::jsonb"
		if strings.Count(base, end) != 1 {
			return ""
		}
		base = strings.Replace(base, end, end+",map_moment_media_payload.proof", 1)
	} else {
		base = strings.Replace(base, ",map_media_payload.proof", ",map_moment_media_payload.proof", 1)
	}
	return strings.Replace(base, "FROM city c CROSS JOIN stamp CROSS JOIN payload", "FROM city c CROSS JOIN stamp CROSS JOIN payload CROSS JOIN map_moment_media_payload", 1)
}
