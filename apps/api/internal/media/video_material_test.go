package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"testing"
	"time"
)

const videoTestID = "11111111-1111-4111-8111-111111111111"
const videoTestOwner = "22222222-2222-4222-8222-222222222222"

func videoTestBytes() []byte {
	box := func(kind string, body []byte) []byte {
		b := make([]byte, 8+len(body))
		binary.BigEndian.PutUint32(b, uint32(len(b)))
		copy(b[4:], kind)
		copy(b[8:], body)
		return b
	}
	return bytes.Join([][]byte{box("ftyp", []byte("isom0000")), box("moov", nil), box("mdat", []byte("synthetic decoder input"))}, nil)
}

type videoTestRepo struct {
	VideoMaterialRepository
	record                             VideoMaterialRecord
	current                            error
	finish                             error
	rejected                           string
	reads, reserves, finishes, cleanup int
}

func (r *videoTestRepo) ReserveHumanVideo(_ context.Context, _ [32]byte, _ identity.Actor, in VideoMaterialInput) (VideoMaterialRecord, error) {
	r.reserves++
	r.record.Receipt.VideoMaterialInput = in
	return r.record, nil
}
func (r *videoTestRepo) ClaimVideoJob(context.Context) (VideoMaterialRecord, error) {
	return r.record, nil
}
func (r *videoTestRepo) VideoJobCurrent(context.Context, VideoMaterialRecord) error { return r.current }
func (r *videoTestRepo) FinishVideoJob(_ context.Context, _ VideoMaterialRecord, d VideoDescriptor) (VideoMaterialReceipt, error) {
	r.finishes++
	if r.finish != nil {
		return VideoMaterialReceipt{}, r.finish
	}
	r.record.Receipt.Status = "ready_private"
	r.record.CleanupPending = true
	r.record.Receipt.Width = d.Width
	r.record.Receipt.Height = d.Height
	return r.record.Receipt, nil
}
func (r *videoTestRepo) RejectVideoJob(_ context.Context, _ VideoMaterialRecord, code string) error {
	r.rejected = code
	return nil
}
func (r *videoTestRepo) FinishVideoCleanup(context.Context, VideoMaterialRecord) error {
	r.cleanup++
	return nil
}
func (r *videoTestRepo) ReadHumanVideo(_ context.Context, _ [32]byte, a identity.Actor, _ string, _ bool, f func(VideoMaterialRecord) ([]byte, error)) ([]byte, error) {
	r.reads++
	if a.ID != r.record.Receipt.OwnerID {
		return nil, content.ErrNotFound
	}
	return f(r.record)
}

type videoTestStorage struct {
	Storage
	data, private                                   []byte
	signed                                          int
	put, deletedPrivate, deletedQuarantine, revoked int
	corrupt                                         bool
	afterPut                                        func()
	cleanupError                                    error
}

func (s *videoTestStorage) SignQuarantineWrite(_ context.Context, in UploadIntent) (UploadTarget, error) {
	s.signed++
	return UploadTarget{URL: "https://storage.invalid/upload", ExpiresAt: in.ExpiresAt}, nil
}
func (s *videoTestStorage) OpenQuarantine(context.Context, string) (io.ReadCloser, ObjectInfo, error) {
	return io.NopCloser(bytes.NewReader(s.data)), ObjectInfo{int64(len(s.data))}, nil
}
func (s *videoTestStorage) PutPrivate(_ context.Context, _ string, r io.Reader) error {
	s.put++
	s.private, _ = io.ReadAll(r)
	if s.corrupt {
		s.private[0] ^= 1
	}
	if s.afterPut != nil {
		s.afterPut()
	}
	return nil
}
func (s *videoTestStorage) OpenPrivate(context.Context, string) (io.ReadCloser, ObjectInfo, error) {
	return io.NopCloser(bytes.NewReader(s.private)), ObjectInfo{int64(len(s.private))}, nil
}
func (s *videoTestStorage) DeletePrivate(context.Context, string) error {
	s.deletedPrivate++
	return s.cleanupError
}
func (s *videoTestStorage) DeleteQuarantine(context.Context, string) error {
	s.deletedQuarantine++
	return s.cleanupError
}
func (s *videoTestStorage) RevokeQuarantineWrite(context.Context, string) error {
	s.revoked++
	return s.cleanupError
}

type videoTestScanner struct {
	verdict ScanVerdict
	err     error
	after   func()
}

func (s videoTestScanner) Scan(context.Context, io.Reader) (ScanVerdict, error) {
	if s.after != nil {
		s.after()
	}
	return s.verdict, s.err
}

type videoTestInspector struct {
	descriptor VideoDescriptor
	err        error
}

func (s videoTestInspector) Inspect(context.Context, []byte) (VideoDescriptor, error) {
	return s.descriptor, s.err
}
func videoTestService() (*VideoMaterialService, *videoTestRepo, *videoTestStorage) {
	b := videoTestBytes()
	h := sha256.Sum256(b)
	st := &videoTestStorage{data: b}
	runtime := VideoRuntime{Storage: st, Scanner: videoTestScanner{verdict: ScanClean}, Inspector: videoTestInspector{descriptor: VideoDescriptor{640, 360, 1000}}, ProviderLabel: "synthetic-test", Region: "test-only", UploadOrigin: "https://storage.invalid"}
	in := VideoMaterialInput{OperationID: videoTestID, MIME: "video/mp4", ByteSize: int64(len(b)), SHA256: hex.EncodeToString(h[:]), DurationMS: 1000, Purpose: VideoMaterialPurpose, ConfirmedPrivateSave: true, StoragePolicyID: runtime.PolicyID()}
	repo := &videoTestRepo{record: VideoMaterialRecord{Receipt: VideoMaterialReceipt{ID: videoTestID, OwnerID: videoTestOwner, VideoMaterialInput: in, Status: "scanning", Revision: 3, UploadExpiresAt: time.Now().UTC().Add(time.Minute), RetainUntil: time.Now().UTC().Add(time.Hour)}, QuarantineKey: "opaque-q", PrivateKey: "opaque-p", LeaseID: videoTestID}}
	return NewVideoMaterialService(repo, runtime), repo, st
}
func TestVideoMaterialExplicitPolicyAndQuarantineOnlySigning(t *testing.T) {
	s, r, st := videoTestService()
	in := r.record.Receipt.VideoMaterialInput
	disabled := NewVideoMaterialService(r, VideoRuntime{})
	if disabled.Capabilities().Available {
		t.Fatal("default provider enabled")
	}
	if _, e := disabled.Create(context.Background(), [32]byte{}, identity.Actor{ID: videoTestOwner}, in); !errors.Is(e, ErrVideoUnavailable) || r.reserves != 0 {
		t.Fatal("disabled write", e)
	}
	bad := in
	bad.ConfirmedPrivateSave = false
	if ValidateVideoMaterialInput(bad) == nil {
		t.Fatal("implicit approval")
	}
	bad = in
	bad.StoragePolicyID = string(bytes.Repeat([]byte("0"), 64))
	if _, e := s.Create(context.Background(), [32]byte{}, identity.Actor{ID: videoTestOwner}, bad); !errors.Is(e, content.ErrConflict) {
		t.Fatal("provider changed", e)
	}
	r.record.Receipt.Status = "quarantine"
	p, e := s.Create(context.Background(), [32]byte{}, identity.Actor{ID: videoTestOwner}, in)
	if e != nil || p.Receipt.Status != "quarantine" || p.UploadURL == "" || st.signed != 1 {
		t.Fatal(p, e)
	}
	r.record.Receipt.Status = "ready_private"
	p, e = s.Create(context.Background(), [32]byte{}, identity.Actor{ID: videoTestOwner}, in)
	if e != nil || p.UploadURL != "" || st.signed != 1 {
		t.Fatal("duplicate produced upload proof", p, e)
	}
}
func TestVideoMaterialWorkerChecksBytesDecoderScannerAndCurrentApproval(t *testing.T) {
	cases := []struct {
		name, code string
		change     func(*VideoMaterialService, *videoTestRepo, *videoTestStorage)
	}{
		{"checksum", "checksum_mismatch", func(_ *VideoMaterialService, _ *videoTestRepo, st *videoTestStorage) { st.data[len(st.data)-1] ^= 1 }},
		{"decoder", "decode_invalid", func(s *VideoMaterialService, _ *videoTestRepo, _ *videoTestStorage) {
			s.Runtime.Inspector = videoTestInspector{err: ErrVideoInvalid}
		}},
		{"long_video", "video_limit", func(s *VideoMaterialService, _ *videoTestRepo, _ *videoTestStorage) {
			s.Runtime.Inspector = videoTestInspector{descriptor: VideoDescriptor{640, 360, 30001}}
		}},
		{"scanner_error", "scan_rejected", func(s *VideoMaterialService, _ *videoTestRepo, _ *videoTestStorage) {
			s.Runtime.Scanner = videoTestScanner{err: errors.New("offline")}
		}},
		{"scanner_unknown", "scan_rejected", func(s *VideoMaterialService, _ *videoTestRepo, _ *videoTestStorage) {
			s.Runtime.Scanner = videoTestScanner{verdict: "pending"}
		}},
		{"retired_during_scan", "source_retired", func(s *VideoMaterialService, r *videoTestRepo, _ *videoTestStorage) {
			s.Runtime.Scanner = videoTestScanner{verdict: ScanClean, after: func() { r.current = identity.ErrUnauthorized }}
		}},
		{"corrupt_private_copy", "storage_unavailable", func(_ *VideoMaterialService, _ *videoTestRepo, st *videoTestStorage) { st.corrupt = true }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, r, st := videoTestService()
			c.change(s, r, st)
			worked, e := s.ProcessNext(context.Background())
			if !worked || e == nil || r.rejected != c.code || r.finishes != 0 {
				t.Fatal("not fail closed", worked, e, r.rejected, r.finishes)
			}
		})
	}
}
func TestVideoMaterialWorkerReadyCleanupAndUnknownCommit(t *testing.T) {
	s, r, st := videoTestService()
	worked, e := s.ProcessNext(context.Background())
	if !worked || e != nil || r.record.Receipt.Status != "ready_private" || st.put != 1 || !bytes.Equal(st.private, st.data) {
		t.Fatal(worked, e, r.record, st)
	}
	if _, e = s.ProcessNext(context.Background()); e != nil || r.cleanup != 1 || st.revoked != 1 || st.deletedPrivate != 0 {
		t.Fatal("ready cleanup deleted private original", e, st)
	}
	s, r, st = videoTestService()
	r.finish = content.ErrUnavailable
	if _, e = s.ProcessNext(context.Background()); e == nil || st.deletedPrivate != 0 || r.rejected != "" {
		t.Fatal("unknown commit destroyed potential ready bytes", e)
	}
	r.record.CleanupPending = true
	r.record.Receipt.Status = "deleting"
	st.cleanupError = errors.New("provider unavailable")
	if _, e = s.ProcessNext(context.Background()); e == nil || r.cleanup != 0 {
		t.Fatal("fake physical deletion", e)
	}
}
func TestVideoMaterialPrivateReadRequiresOwnerReadyPolicyAndChecksum(t *testing.T) {
	s, r, st := videoTestService()
	r.record.Receipt.Status = "ready_private"
	st.private = append([]byte(nil), st.data...)
	a := identity.Actor{ID: videoTestOwner}
	b, e := s.Content(context.Background(), [32]byte{}, a, videoTestID)
	if e != nil || !bytes.Equal(b, st.data) {
		t.Fatal(e)
	}
	if _, e = s.Content(context.Background(), [32]byte{}, identity.Actor{ID: videoTestID}, videoTestID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("peer read", e)
	}
	st.private[len(st.private)-1] ^= 1
	if _, e = s.Content(context.Background(), [32]byte{}, a, videoTestID); e == nil {
		t.Fatal("content hash ignored")
	}
	r.record.Receipt.Status = "deleting"
	if _, e = s.Content(context.Background(), [32]byte{}, a, videoTestID); e == nil {
		t.Fatal("tombstone ignored")
	}
}
func TestVideoMaterialSignatureRejectsMalformedContainers(t *testing.T) {
	valid := videoTestBytes()
	if ValidateMP4Signature(valid) != nil {
		t.Fatal("valid synthetic signature")
	}
	for _, b := range [][]byte{[]byte("not mp4"), valid[:len(valid)-1], append(append([]byte(nil), valid...), 1), bytes.Repeat([]byte("x"), 25)} {
		if ValidateMP4Signature(b) == nil {
			t.Fatal("accepted malformed signature")
		}
	}
}
