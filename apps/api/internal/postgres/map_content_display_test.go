package postgres

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

func TestMapContentDisplayBoundedWholeFrame(t *testing.T) {
	for _, size := range []image.Point{{5760, 7680}, {1280, 1280}, {301, 113}} {
		src := image.NewRGBA(image.Rectangle{Max: size})
		// The large image matches the failing phone image's geometry. A seeded
		// high-entropy smaller input exercises the encoded byte cap as well.
		var seed uint32 = 12345
		for y := 0; y < size.Y; y++ {
			for x := 0; x < size.X; x++ {
				if size.X == 1280 {
					seed = seed*1664525 + 1013904223
					src.SetRGBA(x, y, color.RGBA{uint8(seed), uint8(seed >> 8), uint8(seed >> 16), 255})
				} else {
					src.SetRGBA(x, y, color.RGBA{uint8(x * 255 / size.X), uint8(y * 255 / size.Y), 50, 255})
				}
			}
		}
		var input bytes.Buffer
		if jpeg.Encode(&input, src, &jpeg.Options{Quality: 90}) != nil {
			t.Fatal("fixture encode")
		}
		raw := input.Bytes()
		hash := mm.DigestBytes(raw)
		started := time.Now()
		v, e := mapDisplayEncode(context.Background(), raw, hash)
		elapsed := time.Since(started)
		if e != nil || len(v.bytes) > mapDisplayMaxBytes || v.width > 1280 || v.height > 1280 || v.width < 1 || v.height < 1 || mm.DigestBytes(v.bytes) != v.sha || mm.DigestBytes(raw) != hash {
			t.Fatal("bounded display or immutable original", e)
		}
		if size.X == 5760 {
			t.Logf(`{"syntheticSingleImageWidth":5760,"syntheticSingleImageHeight":7680,"originalBytes":%d,"displayBytes":%d,"coldPixelDecodeScaleEncodeMS":%.3f,"includesDatabaseOrNetwork":false}`, len(raw), len(v.bytes), float64(elapsed.Microseconds())/1000)
		}
		if math.Abs(float64(v.width)/float64(v.height)-float64(size.X)/float64(size.Y)) > 2.0/float64(v.height) {
			t.Fatal("aspect ratio changed")
		}
		decoded, e := jpeg.Decode(bytes.NewReader(v.bytes))
		if e != nil || decoded.Bounds().Dx() != v.width || decoded.Bounds().Dy() != v.height {
			t.Fatal("metadata differs from actual JPEG")
		}
		if size.X != 1280 {
			r0, g0, _, _ := decoded.At(0, 0).RGBA()
			r1, g1, _, _ := decoded.At(v.width-1, v.height-1).RGBA()
			if r0 > 8*257 || g0 > 8*257 || r1 < 245*257 || g1 < 245*257 {
				t.Fatal("source frame cropped")
			}
		}
		again, e := mapDisplayEncode(context.Background(), raw, hash)
		if e != nil || !bytes.Equal(v.bytes, again.bytes) {
			t.Fatal("unstable derivative")
		}
		if _, e = mapDisplayEncode(context.Background(), raw, "wrong"); e == nil {
			t.Fatal("unverified original accepted")
		}
	}
	transparent := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	var input bytes.Buffer
	png.Encode(&input, transparent)
	v, e := mapDisplayEncode(context.Background(), input.Bytes(), mm.DigestBytes(input.Bytes()))
	if e != nil || v.width != 2 || v.height != 3 {
		t.Fatal("small image upscaled", e)
	}
	d, _ := jpeg.Decode(bytes.NewReader(v.bytes))
	r, g, b, _ := d.At(0, 0).RGBA()
	if min(r, g, b) < 250*257 {
		t.Fatal("alpha not composited to white")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = mapDisplayEncode(cancelled, input.Bytes(), mm.DigestBytes(input.Bytes())); e == nil {
		t.Fatal("cancelled decode continued")
	}
}

func TestMapContentDisplayCacheBoundsAndVariantIsolation(t *testing.T) {
	mapDisplayCache.Lock()
	mapDisplayCache.items = map[string]mapMarkerThumbnail{}
	mapDisplayCache.bytes = 0
	mapDisplayCache.Unlock()
	for i := 0; i < 40; i++ {
		saveMapDisplay(string(rune('a'+i)), mapMarkerThumbnail{bytes: make([]byte, mapDisplayMaxBytes)})
	}
	mapDisplayCache.Lock()
	count, size := len(mapDisplayCache.items), mapDisplayCache.bytes
	mapDisplayCache.Unlock()
	if count != 32 || size != 8*1024*1024 {
		t.Fatal("display LRU unbounded")
	}
	if _, ok := cachedMapDisplay("a"); ok {
		t.Fatal("oldest display entry retained")
	}
	if mapDisplayVersion("source") == mapMarkerVersion("source") || mapDisplayVersion("source") == mapDisplayVersion("changed") {
		t.Fatal("source or variant not bound")
	}
	// The next proof/descriptor read must survive pixel-cache eviction without
	// any database or second decode, while retaining a separate metadata bound.
	x := mapContentAsset{kind: "private-image", media: content.MapContentMedia{ID: "display-test", SourceVersion: "original"}}
	key := mapDisplayVersion(x.media.SourceVersion)
	saveMapDisplayMetadata(key, mapMarkerThumbnail{bytes: []byte{1, 2, 3}, width: 960, height: 1280, sha: "verified-sha"})
	r, e := (*Store)(nil).mapDisplayMediaTx(context.Background(), humanWriteFrame{}, x)
	if e != nil || r.Width != 960 || r.Height != 1280 || r.ByteCount != 3 || r.SHA256 != "verified-sha" {
		t.Fatal("metadata eviction caused redundant generation", e)
	}
	for i := 0; i < 4100; i++ {
		saveMapDisplayMetadata(string(rune(i)), mapMarkerThumbnail{width: 1, height: 1})
	}
	mapDisplayMetadataCache.Lock()
	metadataCount := len(mapDisplayMetadataCache.items)
	mapDisplayMetadataCache.Unlock()
	if metadataCount != 4096 {
		t.Fatal("unbounded metadata cache")
	}
}
