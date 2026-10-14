package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
)

func chatTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 200, A: 255})
		}
	}
	var out bytes.Buffer
	if e := png.Encode(&out, img); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}
func chatPNGChunk(kind string, body []byte) []byte {
	b := make([]byte, 12+len(body))
	binary.BigEndian.PutUint32(b, uint32(len(body)))
	copy(b[4:], kind)
	copy(b[8:], body)
	binary.BigEndian.PutUint32(b[len(b)-4:], crc32.ChecksumIEEE(b[4:len(b)-4]))
	return b
}
func TestChatImageActualPixelsAndMetadataRemoval(t *testing.T) {
	in := chatTestPNG(t, 320, 180)
	marker := []byte("description\x00private source filename.jpg")
	in = append(append(append([]byte(nil), in[:33]...), chatPNGChunk("tEXt", marker)...), in[33:]...)
	out, e := (&LocalChatImageDerivativeBuilder{}).Build(context.Background(), in, "image/png")
	if e != nil || !connection.ValidImageMetadata(out.Metadata) {
		t.Fatal("actual PNG codec", e)
	}
	if out.Metadata.Width != 320 || out.Metadata.Height != 180 || out.Metadata.Thumbnail.Width != 256 || out.Metadata.Thumbnail.Height != 144 {
		t.Fatal("actual aspect dimensions")
	}
	if bytes.Contains(out.Full, marker) || bytes.Contains(out.Thumbnail, marker) {
		t.Fatal("source metadata retained")
	}
	for _, v := range []struct {
		b     []byte
		m     connection.ImageDerivativeMetadata
		thumb bool
	}{{out.Full, out.Metadata.ImageDerivativeMetadata, false}, {out.Thumbnail, out.Metadata.Thumbnail, true}} {
		if !connection.ValidImageContent(v.m, v.b, v.thumb) || ValidateChatImageDerivative(context.Background(), v.b, v.m.MIME, v.m.Width, v.m.Height) != nil {
			t.Fatal("actual pixels/header/SHA mismatch")
		}
	}
}
func TestChatImageRejectsAnimationMIMEAndCancellation(t *testing.T) {
	in := chatTestPNG(t, 2, 2)
	animated := append(append(append([]byte(nil), in[:33]...), chatPNGChunk("acTL", []byte{0, 0, 0, 1, 0, 0, 0, 0})...), in[33:]...)
	if _, e := (&LocalChatImageDerivativeBuilder{}).Build(context.Background(), animated, "image/png"); !errors.Is(e, ErrImageType) {
		t.Fatal("APNG accepted", e)
	}
	if _, e := (&LocalChatImageDerivativeBuilder{}).Build(context.Background(), in, "image/jpeg"); !errors.Is(e, ErrImageType) {
		t.Fatal("declared MIME trusted", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := (&LocalChatImageDerivativeBuilder{}).Build(ctx, in, "image/png"); !errors.Is(e, context.Canceled) {
		t.Fatal("canceled pixels retained", e)
	}
	large := append([]byte(nil), in...)
	binary.BigEndian.PutUint32(large[16:20], 8193)
	binary.BigEndian.PutUint32(large[29:33], crc32.ChecksumIEEE(large[12:29]))
	if _, e := (&LocalChatImageDerivativeBuilder{}).Build(context.Background(), large, "image/png"); !errors.Is(e, ErrImageLimit) {
		t.Fatal("dimensions allocated before bound", e)
	}
}
func TestChatImageJPEGStaysJPEGWithIndependentPNGThumbnail(t *testing.T) {
	var b bytes.Buffer
	if e := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 64, 400)), &jpeg.Options{Quality: 95}); e != nil {
		t.Fatal(e)
	}
	out, e := (&LocalChatImageDerivativeBuilder{}).Build(context.Background(), b.Bytes(), "image/jpeg")
	if e != nil || out.Metadata.MIME != "image/jpeg" || out.Metadata.Thumbnail.MIME != "image/png" || out.Metadata.Thumbnail.Width != 41 || out.Metadata.Thumbnail.Height != 256 {
		t.Fatal("JPEG full / bounded PNG thumbnail", e)
	}
	if e = ValidateChatImageDerivative(context.Background(), out.Full, "image/jpeg", 64, 400); e != nil {
		t.Fatal("actual JPEG derivative", e)
	}
}
