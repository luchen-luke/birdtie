package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

func imageUnitPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 2; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: uint8(10 + y*2 + x), G: 90, B: 200, A: 255})
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}

func imageUnitChunk(kind string, payload []byte) []byte {
	b := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(b, uint32(len(payload)))
	copy(b[4:8], kind)
	copy(b[8:], payload)
	binary.BigEndian.PutUint32(b[8+len(payload):], crc32.ChecksumIEEE(b[4:8+len(payload)]))
	return b
}

func imageUnitTIFF(orientation uint16, big bool) []byte {
	b := make([]byte, 26)
	var order binary.ByteOrder = binary.LittleEndian
	copy(b, "II")
	if big {
		order = binary.BigEndian
		copy(b, "MM")
	}
	order.PutUint16(b[2:], 42)
	order.PutUint32(b[4:], 8)
	order.PutUint16(b[8:], 1)
	order.PutUint16(b[10:], 0x112)
	order.PutUint16(b[12:], 3)
	order.PutUint32(b[14:], 1)
	order.PutUint16(b[18:], orientation)
	return b
}

func imageUnitPNGInsert(pngBytes []byte, chunk []byte) []byte {
	b := append([]byte{}, pngBytes[:33]...)
	b = append(b, chunk...)
	return append(b, pngBytes[33:]...)
}

func imageUnitBuild(t *testing.T, data []byte, mime string) []byte {
	t.Helper()
	r, got, e := (&LocalImageDerivativeBuilder{}).BuildPublic(context.Background(), bytes.NewReader(data), mime)
	if e != nil || r == nil || got != mime {
		t.Fatal("codec failed", got, e)
	}
	defer r.Close()
	b, e := io.ReadAll(r)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestMediaImageUnitOrientationAndMetadataRemoval(t *testing.T) {
	// Each explicit matrix is the six original pixels after EXIF orientation.
	mats := [][][]uint8{
		{{10, 11}, {12, 13}, {14, 15}}, {{11, 10}, {13, 12}, {15, 14}},
		{{15, 14}, {13, 12}, {11, 10}}, {{14, 15}, {12, 13}, {10, 11}},
		{{10, 12, 14}, {11, 13, 15}}, {{14, 12, 10}, {15, 13, 11}},
		{{15, 13, 11}, {14, 12, 10}}, {{11, 13, 15}, {10, 12, 14}},
	}
	for o := 1; o <= 8; o++ {
		for _, big := range []bool{false, true} {
			t.Run(fmt.Sprintf("orientation%d_big%v", o, big), func(t *testing.T) {
				meta := append(imageUnitTIFF(uint16(o), big), []byte("GPS_PRIVATE_CANARY_CAPTURE_TIME_NOT_VISIT")...)
				input := imageUnitPNGInsert(imageUnitPNG(t), imageUnitChunk("eXIf", meta))
				input = imageUnitPNGInsert(input, imageUnitChunk("tEXt", []byte("Comment\x00OCR_ORIGINAL_PRIVATE_CANARY")))
				out := imageUnitBuild(t, input, "image/png")
				for _, needle := range []string{"eXIf", "GPS_PRIVATE_CANARY", "tEXt", "OCR_ORIGINAL_PRIVATE_CANARY"} {
					if bytes.Contains(out, []byte(needle)) {
						t.Fatal("metadata retained", needle)
					}
				}
				im, e := png.Decode(bytes.NewReader(out))
				if e != nil {
					t.Fatal(e)
				}
				want := mats[o-1]
				if im.Bounds().Dx() != len(want[0]) || im.Bounds().Dy() != len(want) {
					t.Fatal("orientation size", im.Bounds())
				}
				for y, row := range want {
					for x, r := range row {
						got := color.NRGBAModel.Convert(im.At(x, y)).(color.NRGBA)
						if got != (color.NRGBA{r, 90, 200, 255}) {
							t.Fatal(x, y, got, r)
						}
					}
				}
			})
		}
	}
}

func TestMediaImageUnitJPEGReencodeDropsOriginalMetadata(t *testing.T) {
	im, e := png.Decode(bytes.NewReader(imageUnitPNG(t)))
	if e != nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	if e = jpeg.Encode(&b, im, nil); e != nil {
		t.Fatal(e)
	}
	meta := append([]byte("Exif\x00\x00"), imageUnitTIFF(6, false)...)
	meta = append(meta, []byte("GPS_PERSONAL_ADDRESS_CANARY")...)
	segment := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(meta)+2))
	segment = append(segment, meta...)
	in := append([]byte{}, b.Bytes()[:2]...)
	in = append(in, segment...)
	in = append(in, b.Bytes()[2:]...)
	out := imageUnitBuild(t, in, "image/jpeg")
	if bytes.Contains(out, []byte("Exif")) || bytes.Contains(out, []byte("GPS_PERSONAL_ADDRESS_CANARY")) {
		t.Fatal("EXIF leaked")
	}
	cfg, e := jpeg.DecodeConfig(bytes.NewReader(out))
	if e != nil || cfg.Width != 3 || cfg.Height != 2 {
		t.Fatal("JPEG orientation", cfg, e)
	}
}

func TestMediaImageUnitRejectsMalformedAndNonstaticContent(t *testing.T) {
	base := imageUnitPNG(t)
	badOffset := imageUnitTIFF(1, false)
	binary.LittleEndian.PutUint32(badOffset[4:], 0xffffffff)
	badCount := imageUnitTIFF(1, false)
	binary.LittleEndian.PutUint16(badCount[8:], 0xffff)
	badType := imageUnitTIFF(1, false)
	binary.LittleEndian.PutUint16(badType[12:], 4)
	badValueCount := imageUnitTIFF(1, false)
	binary.LittleEndian.PutUint32(badValueCount[14:], 2)
	badDuplicate := make([]byte, 38)
	copy(badDuplicate, imageUnitTIFF(1, false))
	binary.LittleEndian.PutUint16(badDuplicate[8:], 2)
	copy(badDuplicate[22:], badDuplicate[10:22])
	cases := []struct {
		name, mime string
		data       []byte
		want       error
	}{
		{"svg", "image/svg+xml", []byte("<svg/>"), ErrImageType},
		{"gif", "image/gif", []byte("GIF89a"), ErrImageType},
		{"mismatchedMime", "image/jpeg", base, ErrImageType},
		{"mimeAlias", "image/png; charset=utf-8", base, ErrImageType},
		{"truncated", "image/png", base[:40], ErrImageInvalid},
		{"badOffset", "image/png", imageUnitPNGInsert(base, imageUnitChunk("eXIf", badOffset)), ErrImageInvalid},
		{"badCount", "image/png", imageUnitPNGInsert(base, imageUnitChunk("eXIf", badCount)), ErrImageInvalid},
		{"badType", "image/png", imageUnitPNGInsert(base, imageUnitChunk("eXIf", badType)), ErrImageInvalid},
		{"badValueCount", "image/png", imageUnitPNGInsert(base, imageUnitChunk("eXIf", badValueCount)), ErrImageInvalid},
		{"duplicateOrientationTag", "image/png", imageUnitPNGInsert(base, imageUnitChunk("eXIf", badDuplicate)), ErrImageInvalid},
		{"badOrientation", "image/png", imageUnitPNGInsert(base, imageUnitChunk("eXIf", imageUnitTIFF(9, false))), ErrImageInvalid},
		{"duplicateExif", "image/png", imageUnitPNGInsert(imageUnitPNGInsert(base, imageUnitChunk("eXIf", imageUnitTIFF(1, false))), imageUnitChunk("eXIf", imageUnitTIFF(1, false))), ErrImageInvalid},
		{"apng", "image/png", imageUnitPNGInsert(base, imageUnitChunk("acTL", make([]byte, 8))), ErrImageType},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, m, e := (&LocalImageDerivativeBuilder{}).BuildPublic(context.Background(), bytes.NewReader(c.data), c.mime)
			if !errors.Is(e, c.want) || r != nil || m != "" {
				t.Fatal("unsafe acceptance", r, m, e, c.want)
			}
		})
	}
}

func TestMediaImageUnitInputPixelAndOutputLimits(t *testing.T) {
	t.Run("input", func(t *testing.T) {
		_, _, e := (&LocalImageDerivativeBuilder{}).BuildPublic(context.Background(), bytes.NewReader(make([]byte, MaxImageInputBytes+1)), "image/png")
		if !errors.Is(e, ErrImageLimit) {
			t.Fatal(e)
		}
	})
	for _, dims := range [][2]uint32{{8193, 1}, {4097, 4096}, {1, 8193}} {
		t.Run(fmt.Sprint(dims), func(t *testing.T) {
			p := append([]byte{}, imageUnitPNG(t)...)
			binary.BigEndian.PutUint32(p[16:], dims[0])
			binary.BigEndian.PutUint32(p[20:], dims[1])
			binary.BigEndian.PutUint32(p[29:], crc32.ChecksumIEEE(p[12:29]))
			_, _, e := (&LocalImageDerivativeBuilder{}).BuildPublic(context.Background(), bytes.NewReader(p), "image/png")
			if !errors.Is(e, ErrImageLimit) {
				t.Fatal("did not reject before IDAT allocation", e)
			}
		})
	}
	t.Run("outputBoundedWriter", func(t *testing.T) {
		w := &imageContextWriter{ctx: context.Background()}
		n, e := w.Write(make([]byte, MaxImageOutputBytes+1))
		if n != 0 || !errors.Is(e, ErrImageLimit) || w.buf.Len() != 0 {
			t.Fatal(n, e, w.buf.Len())
		}
	})
}

type imageUnitReadError struct{}

func (imageUnitReadError) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestMediaImageUnitCancellationAndNoResultOnReadFailure(t *testing.T) {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	r, m, e := (&LocalImageDerivativeBuilder{}).BuildPublic(c, bytes.NewReader(imageUnitPNG(t)), "image/png")
	if !errors.Is(e, context.Canceled) || r != nil || m != "" {
		t.Fatal(r, m, e)
	}
	r, m, e = (&LocalImageDerivativeBuilder{}).BuildPublic(context.Background(), imageUnitReadError{}, "image/png")
	if !errors.Is(e, io.ErrUnexpectedEOF) || r != nil || m != "" {
		t.Fatal(r, m, e)
	}
	r, m, e = (&LocalImageDerivativeBuilder{}).BuildPublic(nil, bytes.NewReader(nil), "image/png")
	if !errors.Is(e, ErrImageInvalid) || r != nil || m != "" {
		t.Fatal(r, m, e)
	}
	_, e = orientImage(c, image.NewRGBA(image.Rect(0, 0, 2, 3)), 6)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
