package postgres

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"sync"
	"sync/atomic"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

const mapDisplayMaxBytes = 256 * 1024

// Aggregate counters used by the isolated cold-path regression. No content or
// account identifiers are recorded, and counters never affect permissions.
var mapDisplayDecodeCount atomic.Uint64
var mapDisplayRawReadBytes atomic.Uint64
var mapContentHeaderReadBytes atomic.Uint64

// Reading derivatives are separate from marker160. Neither viewport metadata
// nor marker generation calls this cache. Entries carry no authority: every
// descriptor and byte read first resolves the current moment/media permission.
var mapDisplayCache = struct {
	sync.Mutex
	items map[string]mapMarkerThumbnail
	bytes int
}{items: map[string]mapMarkerThumbnail{}}

// Keep only the small deterministic descriptor after pixel eviction. Without
// this separate bound a detail with more than 32 images would re-encode every
// image during its response proof, even though their current sources match.
type mapDisplayMetadata struct {
	width, height, byteCount int
	sha                      string
	used                     time.Time
}

var mapDisplayMetadataCache = struct {
	sync.Mutex
	items map[string]mapDisplayMetadata
}{items: map[string]mapDisplayMetadata{}}

func cachedMapDisplayMetadata(key string) (mapDisplayMetadata, bool) {
	mapDisplayMetadataCache.Lock()
	defer mapDisplayMetadataCache.Unlock()
	v, ok := mapDisplayMetadataCache.items[key]
	if ok {
		v.used = time.Now()
		mapDisplayMetadataCache.items[key] = v
	}
	return v, ok
}

func saveMapDisplayMetadata(key string, v mapMarkerThumbnail) mapDisplayMetadata {
	mapDisplayMetadataCache.Lock()
	defer mapDisplayMetadataCache.Unlock()
	m := mapDisplayMetadata{width: v.width, height: v.height, byteCount: len(v.bytes), sha: v.sha, used: time.Now()}
	mapDisplayMetadataCache.items[key] = m
	for len(mapDisplayMetadataCache.items) > 4096 {
		oldest := ""
		var at time.Time
		for k, x := range mapDisplayMetadataCache.items {
			if oldest == "" || x.used.Before(at) {
				oldest, at = k, x.used
			}
		}
		delete(mapDisplayMetadataCache.items, oldest)
	}
	return m
}

func mapDisplayVersion(source string) string {
	return content.MomentControlHash([]any{source, "display1280-v1"})
}
func cachedMapDisplay(key string) (mapMarkerThumbnail, bool) {
	mapDisplayCache.Lock()
	defer mapDisplayCache.Unlock()
	v, ok := mapDisplayCache.items[key]
	if ok {
		v.used = time.Now()
		mapDisplayCache.items[key] = v
	}
	return v, ok
}
func saveMapDisplay(key string, v mapMarkerThumbnail) {
	mapDisplayCache.Lock()
	defer mapDisplayCache.Unlock()
	if old, ok := mapDisplayCache.items[key]; ok {
		mapDisplayCache.bytes -= len(old.bytes)
	}
	v.used = time.Now()
	mapDisplayCache.items[key] = v
	mapDisplayCache.bytes += len(v.bytes)
	for len(mapDisplayCache.items) > 32 || mapDisplayCache.bytes > 8*1024*1024 {
		oldest := ""
		var at time.Time
		for k, x := range mapDisplayCache.items {
			if oldest == "" || x.used.Before(at) {
				oldest, at = k, x.used
			}
		}
		mapDisplayCache.bytes -= len(mapDisplayCache.items[oldest].bytes)
		delete(mapDisplayCache.items, oldest)
	}
}

// Area averaging retains the whole source frame and reduces high-frequency
// aliasing in large photos. Alpha is composited onto white, not discarded.
func mapDisplayScale(ctx context.Context, src image.Image, edge int) (*image.RGBA, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if max(w, h) > edge {
		scale := float64(edge) / float64(max(w, h))
		w, h = max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		if e := ctx.Err(); e != nil {
			return nil, content.ErrUnavailable
		}
		y0, y1 := y*b.Dy()/h, max(y*b.Dy()/h+1, (y+1)*b.Dy()/h)
		for x := 0; x < w; x++ {
			x0, x1 := x*b.Dx()/w, max(x*b.Dx()/w+1, (x+1)*b.Dx()/w)
			var rr, gg, bb uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, bl, a := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					rr += uint64(r + 65535 - a)
					gg += uint64(g + 65535 - a)
					bb += uint64(bl + 65535 - a)
				}
			}
			n := uint64((x1 - x0) * (y1 - y0))
			out.SetRGBA(x, y, color.RGBA{uint8(rr / n >> 8), uint8(gg / n >> 8), uint8(bb / n >> 8), 255})
		}
	}
	return out, nil
}

func mapDisplayEncode(ctx context.Context, raw []byte, expected string) (mapMarkerThumbnail, error) {
	if len(raw) < 1 || len(raw) > 16*1024*1024 || mm.DigestBytes(raw) != expected || ctx.Err() != nil {
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 48*1024*1024 {
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	mapDisplayDecodeCount.Add(1)
	src, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	small, e := mapDisplayScale(ctx, src, 1280)
	if e != nil {
		return mapMarkerThumbnail{}, e
	}
	// Prefer 82-quality at 1280; reduce quality, then resolution if needed.
	// No image silently disappears when its initial encoding exceeds budget.
	for {
		for _, quality := range []int{82, 72, 62} {
			if ctx.Err() != nil {
				return mapMarkerThumbnail{}, content.ErrUnavailable
			}
			var out bytes.Buffer
			if jpeg.Encode(&out, small, &jpeg.Options{Quality: quality}) != nil {
				return mapMarkerThumbnail{}, content.ErrUnavailable
			}
			if out.Len() <= mapDisplayMaxBytes {
				return mapMarkerThumbnail{bytes: out.Bytes(), width: small.Bounds().Dx(), height: small.Bounds().Dy(), sha: mm.DigestBytes(out.Bytes())}, nil
			}
		}
		edge := max(small.Bounds().Dx(), small.Bounds().Dy()) * 3 / 4
		if edge < 1 {
			return mapMarkerThumbnail{}, content.ErrUnavailable
		}
		small, e = mapDisplayScale(ctx, small, edge)
		if e != nil {
			return mapMarkerThumbnail{}, e
		}
	}
}

func (s *Store) mapDisplayBytesTx(ctx context.Context, f humanWriteFrame, x mapContentAsset) (mapMarkerThumbnail, error) {
	if x.kind != "private-image" || x.media.Kind != "IMAGE" {
		return mapMarkerThumbnail{}, content.ErrInvalid
	}
	key := mapDisplayVersion(x.media.SourceVersion)
	if v, ok := cachedMapDisplay(key); ok {
		return v, nil
	}
	select {
	case mapMarkerDecodes <- struct{}{}:
	case <-ctx.Done():
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	defer func() { <-mapMarkerDecodes }()
	if v, ok := cachedMapDisplay(key); ok {
		return v, nil
	}
	var raw []byte
	e := f.tx.QueryRow(ctx, `SELECT derivative FROM private_moment_images WHERE id=$1 AND status='ready_private' AND retain_until>clock_timestamp()`, x.blobID).Scan(&raw)
	defer clear(raw)
	if e != nil || len(raw) != x.media.ByteCount {
		return mapMarkerThumbnail{}, content.ErrUnavailable
	}
	mapDisplayRawReadBytes.Add(uint64(len(raw)))
	v, e := mapDisplayEncode(ctx, raw, x.media.SHA256)
	if e == nil {
		saveMapDisplay(key, v)
	}
	return v, e
}

func (s *Store) mapDisplayMediaTx(ctx context.Context, f humanWriteFrame, x mapContentAsset) (content.MapContentMedia, error) {
	r := x.media
	if x.kind != "private-image" {
		return r, nil
	}
	key := mapDisplayVersion(x.media.SourceVersion)
	m, ok := cachedMapDisplayMetadata(key)
	if !ok {
		v, e := s.mapDisplayBytesTx(ctx, f, x)
		if e != nil {
			return r, e
		}
		m = saveMapDisplayMetadata(key, v)
	}
	r.MIMEType, r.Width, r.Height = "image/jpeg", m.width, m.height
	r.ByteCount, r.SHA256 = m.byteCount, m.sha
	r.SourceVersion = key
	r.ContentPath = mapMediaPath(x.mid, r.ID, r.SourceVersion) + "&variant=display"
	return r, nil
}

type mapDisplayDescriptorProof struct {
	s                      *Store
	d                      [32]byte
	a                      identity.Actor
	moment, originalSource string
	media                  content.MapContentMedia
	until                  time.Time
}

func (p *mapDisplayDescriptorProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.until.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	v, e := p.s.ReadHumanMapContentDisplayDescriptor(ctx, p.d, p.a, p.moment, p.media.ID, p.originalSource)
	if e != nil {
		return e
	}
	if v.ValidUntil.Before(p.until) || content.MomentControlHash(v.Media) != content.MomentControlHash(p.media) {
		return identity.ErrControlChanged
	}
	return nil
}

func (s *Store) ReadHumanMapContentDisplayDescriptor(ctx context.Context, d [32]byte, a identity.Actor, momentID, mediaID, originalSource string) (content.MapContentDisplayDescriptor, error) {
	r := content.MapContentDisplayDescriptor{SchemaVersion: content.MapContentDisplaySchema, ViewerAccountID: a.ID, MomentID: momentID, OriginalSourceVersion: originalSource}
	if !identity.HumanControlUUID(momentID) || !identity.HumanControlUUID(mediaID) || !content.MomentControlHex(originalSource) {
		return r, content.ErrInvalid
	}
	ctx, f, e := s.beginMapContent(ctx, d, a)
	if e != nil {
		return r, e
	}
	defer f.tx.Rollback(context.Background())
	filter, e := mapContentTargetFilter(ctx, f, "moments", momentID)
	if e != nil {
		return r, e
	}
	v, e := readMapContentSnapshot(ctx, f, filter)
	if e != nil {
		return r, e
	}
	m := v.moments[momentID]
	if m == nil {
		return r, content.ErrNotFound
	}
	// Only the selected attachment's header is loaded. The retained image's
	// exact hash/version and current legacy/self permissions remain unchanged.
	assets, e := s.readMapContentMomentMediaTx(ctx, f, &v, m, mediaID)
	if e != nil {
		return r, e
	}
	var chosen *mapContentAsset
	for i := range assets {
		x := &assets[i]
		if x.kind == "private-image" && x.media.ID == mediaID && x.media.SourceVersion == originalSource {
			chosen = x
			break
		}
	}
	if chosen == nil {
		return r, content.ErrNotFound
	}
	r.ObservedAt, r.ValidUntil = v.at, v.until
	shrinkMapLease(&r.ValidUntil, chosen.until)
	r.Media, e = s.mapDisplayMediaTx(ctx, f, *chosen)
	if e != nil {
		return r, e
	}
	if !r.Valid(time.Now().UTC()) {
		return r, content.ErrUnavailable
	}
	if e = s.finishMapContent(ctx, f, v, r.ValidUntil); e != nil {
		return r, e
	}
	r.ResponseProof = &mapDisplayDescriptorProof{s: s, d: d, a: a, moment: momentID, originalSource: originalSource, media: r.Media, until: r.ValidUntil}
	return r, nil
}
