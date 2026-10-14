package media

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// These adapters talk to explicitly configured storage/scanner services. They
// choose no cloud account or production region, and never invent a clean scan.
type HTTPVideoStorage struct {
	BaseURL, Token, UploadOrigin string
	Client                       *http.Client
	Development                  bool
}
type HTTPVideoScanner struct {
	URL, Token  string
	Client      *http.Client
	Development bool
}
type FFmpegVideoInspector struct{ FFprobePath, FFmpegPath, TempDir string }

func videoServiceURL(raw string, dev bool) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, ErrVideoUnavailable
	}
	if u.Scheme == "https" {
		return u, nil
	}
	ip := net.ParseIP(u.Hostname())
	if !dev || u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return nil, ErrVideoUnavailable
	}
	return u, nil
}
func videoHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copy.Timeout == 0 || copy.Timeout > 25*time.Second {
		copy.Timeout = 25 * time.Second
	}
	return &copy
}
func videoObjectKey(key string) bool {
	return len(key) > 0 && len(key) <= 160 && !strings.Contains(key, "..") && !strings.HasPrefix(key, "/") && !strings.ContainsAny(key, "\\\x00?#")
}
func (s *HTTPVideoStorage) request(ctx context.Context, method, suffix string, body io.Reader) (*http.Response, error) {
	if s == nil || s.Token == "" {
		return nil, ErrVideoUnavailable
	}
	u, e := videoServiceURL(s.BaseURL, s.Development)
	if e != nil {
		return nil, e
	}
	u.Path = strings.TrimRight(u.Path, "/") + suffix
	req, e := http.NewRequestWithContext(ctx, method, u.String(), body)
	if e != nil {
		return nil, ErrVideoUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/octet-stream")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	r, e := videoHTTPClient(s.Client).Do(req)
	if e != nil {
		return nil, ErrVideoUnavailable
	}
	return r, nil
}
func (s *HTTPVideoStorage) SignQuarantineWrite(ctx context.Context, in UploadIntent) (UploadTarget, error) {
	if !ValidPrivateImageID(in.ID) || !ValidPrivateImageID(in.OwnerID) || !videoObjectKey(in.ObjectKey) || in.MIMEType != "video/mp4" || in.MaxBytes < 1 || in.MaxBytes > MaxVideoBytes || !privateImageSHA.MatchString(in.SHA256Hex) || !in.ExpiresAt.After(time.Now().UTC()) || in.ExpiresAt.After(time.Now().UTC().Add(VideoUploadTTL)) {
		return UploadTarget{}, ErrVideoInvalid
	}
	raw, _ := json.Marshal(map[string]any{"id": in.ID, "ownerId": in.OwnerID, "objectKey": in.ObjectKey, "mimeType": in.MIMEType, "maxBytes": in.MaxBytes, "sha256": in.SHA256Hex, "expiresAt": in.ExpiresAt})
	r, e := s.request(ctx, "POST", "/v1/quarantine-write-intents", bytes.NewReader(raw))
	if e != nil {
		return UploadTarget{}, e
	}
	defer r.Body.Close()
	if r.StatusCode != 201 {
		return UploadTarget{}, ErrVideoUnavailable
	}
	var wire struct {
		URL       string            `json:"url"`
		Headers   map[string]string `json:"headers"`
		ExpiresAt time.Time         `json:"expiresAt"`
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 16*1024+1))
	if e != nil || len(b) > 16*1024 || json.Unmarshal(b, &wire) != nil {
		return UploadTarget{}, ErrVideoUnavailable
	}
	target, e := url.Parse(wire.URL)
	origin, e2 := videoServiceURL(s.UploadOrigin, s.Development)
	if e != nil || e2 != nil || target.User != nil || target.Scheme != origin.Scheme || target.Host != origin.Host || target.Fragment != "" || !wire.ExpiresAt.After(time.Now().UTC()) || wire.ExpiresAt.After(in.ExpiresAt) || len(wire.Headers) > 12 {
		return UploadTarget{}, ErrVideoUnavailable
	}
	for k, v := range wire.Headers {
		if k == "" || len(k) > 128 || len(v) > 2048 || strings.ContainsAny(k+v, "\r\n") || strings.Contains(v, s.Token) || strings.EqualFold(k, "Host") || strings.EqualFold(k, "Cookie") {
			return UploadTarget{}, ErrVideoUnavailable
		}
	}
	return UploadTarget{wire.URL, wire.Headers, wire.ExpiresAt}, nil
}
func (s *HTTPVideoStorage) open(ctx context.Context, scope, key string) (io.ReadCloser, ObjectInfo, error) {
	if !videoObjectKey(key) {
		return nil, ObjectInfo{}, ErrVideoInvalid
	}
	r, e := s.request(ctx, "GET", "/v1/"+scope+"/"+url.PathEscape(key), nil)
	if e != nil {
		return nil, ObjectInfo{}, e
	}
	if r.StatusCode != 200 {
		r.Body.Close()
		return nil, ObjectInfo{}, ErrVideoUnavailable
	}
	size := r.ContentLength
	if size < 0 {
		size, e = strconv.ParseInt(r.Header.Get("X-Birdtie-Object-Bytes"), 10, 64)
	}
	if e != nil || size < 1 || size > MaxVideoBytes {
		r.Body.Close()
		return nil, ObjectInfo{}, ErrVideoInvalid
	}
	return r.Body, ObjectInfo{ByteSize: size}, nil
}
func (s *HTTPVideoStorage) OpenQuarantine(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	return s.open(ctx, "quarantine", key)
}
func (s *HTTPVideoStorage) OpenPrivate(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	return s.open(ctx, "private", key)
}
func (s *HTTPVideoStorage) PutPrivate(ctx context.Context, key string, body io.Reader) error {
	if !videoObjectKey(key) {
		return ErrVideoInvalid
	}
	b, e := io.ReadAll(io.LimitReader(body, MaxVideoBytes+1))
	if e != nil || len(b) < 1 || int64(len(b)) > MaxVideoBytes {
		return ErrVideoInvalid
	}
	r, e := s.request(ctx, "PUT", "/v1/private/"+url.PathEscape(key), bytes.NewReader(b))
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 && r.StatusCode != 201 {
		return ErrVideoUnavailable
	}
	return nil
}
func (*HTTPVideoStorage) PutPublicDerivative(context.Context, string, io.Reader) error {
	return ErrVideoUnavailable
}
func (s *HTTPVideoStorage) remove(ctx context.Context, scope, key string) error {
	if !videoObjectKey(key) {
		return ErrVideoInvalid
	}
	r, e := s.request(ctx, "DELETE", "/v1/"+scope+"/"+url.PathEscape(key), nil)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 204 && r.StatusCode != 404 {
		return ErrVideoUnavailable
	}
	return nil
}
func (s *HTTPVideoStorage) DeleteQuarantine(ctx context.Context, key string) error {
	return s.remove(ctx, "quarantine", key)
}
func (s *HTTPVideoStorage) RevokeQuarantineWrite(ctx context.Context, id string) error {
	if !ValidPrivateImageID(id) {
		return ErrVideoInvalid
	}
	r, e := s.request(ctx, "DELETE", "/v1/quarantine-write-intents/"+id, nil)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	if r.StatusCode != 204 && r.StatusCode != 404 {
		return ErrVideoUnavailable
	}
	return nil
}
func (s *HTTPVideoStorage) DeletePrivate(ctx context.Context, key string) error {
	return s.remove(ctx, "private", key)
}
func (*HTTPVideoStorage) DeletePublicDerivative(context.Context, string) error {
	return ErrVideoUnavailable
}
func (s *HTTPVideoScanner) Scan(ctx context.Context, reader io.Reader) (ScanVerdict, error) {
	if s == nil || s.Token == "" {
		return ScanRejected, ErrVideoUnavailable
	}
	u, e := videoServiceURL(s.URL, s.Development)
	if e != nil {
		return ScanRejected, e
	}
	b, e := io.ReadAll(io.LimitReader(reader, MaxVideoBytes+1))
	if e != nil || len(b) < 1 || int64(len(b)) > MaxVideoBytes {
		return ScanRejected, ErrVideoInvalid
	}
	req, e := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(b))
	if e != nil {
		return ScanRejected, ErrVideoUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "video/mp4")
	r, e := videoHTTPClient(s.Client).Do(req)
	if e != nil {
		return ScanRejected, ErrVideoUnavailable
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return ScanRejected, ErrVideoUnavailable
	}
	result, e := io.ReadAll(io.LimitReader(r.Body, 1025))
	var p struct {
		Verdict string `json:"verdict"`
	}
	if e != nil || len(result) > 1024 || json.Unmarshal(result, &p) != nil || p.Verdict != "clean" {
		return ScanRejected, ErrVideoInvalid
	}
	return ScanClean, nil
}

type boundedVideoOutput struct {
	bytes.Buffer
	limit int
}

func (w *boundedVideoOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.Len() {
		return 0, ErrVideoInvalid
	}
	return w.Buffer.Write(p)
}
func (s *FFmpegVideoInspector) Inspect(ctx context.Context, b []byte) (VideoDescriptor, error) {
	if s == nil || !filepath.IsAbs(s.FFprobePath) || !filepath.IsAbs(s.FFmpegPath) || ValidateMP4Signature(b) != nil {
		return VideoDescriptor{}, ErrVideoInvalid
	}
	f, e := os.CreateTemp(s.TempDir, "birdtie-video-*.mp4")
	if e != nil {
		return VideoDescriptor{}, ErrVideoUnavailable
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return VideoDescriptor{}, ErrVideoUnavailable
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return VideoDescriptor{}, ErrVideoUnavailable
	}
	if e = f.Close(); e != nil {
		return VideoDescriptor{}, ErrVideoUnavailable
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out := &boundedVideoOutput{limit: 64 * 1024}
	stderr := &boundedVideoOutput{limit: 64 * 1024}
	cmd := exec.CommandContext(probeCtx, s.FFprobePath, "-v", "error", "-max_alloc", "33554432", "-protocol_whitelist", "file,pipe", "-enable_drefs", "0", "-use_absolute_path", "0", "-max_streams", "2", "-show_entries", "format=duration,format_name:stream=codec_type,codec_name,width,height,r_frame_rate", "-of", "json", name)
	cmd.Stdout = out
	cmd.Stderr = stderr
	if cmd.Run() != nil {
		return VideoDescriptor{}, ErrVideoInvalid
	}
	var p struct {
		Format struct {
			Duration string `json:"duration"`
			Name     string `json:"format_name"`
		} `json:"format"`
		Streams []struct {
			Type   string `json:"codec_type"`
			Codec  string `json:"codec_name"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			FPS    string `json:"r_frame_rate"`
		} `json:"streams"`
	}
	if json.Unmarshal(out.Bytes(), &p) != nil || len(p.Streams) < 1 || len(p.Streams) > 2 {
		return VideoDescriptor{}, ErrVideoInvalid
	}
	duration, e := strconv.ParseFloat(p.Format.Duration, 64)
	if e != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 30 {
		return VideoDescriptor{}, ErrVideoInvalid
	}
	descriptor := VideoDescriptor{DurationMS: int64(math.Round(duration * 1000))}
	videos, audios := 0, 0
	for _, stream := range p.Streams {
		switch stream.Type {
		case "video":
			videos++
			if stream.Codec != "h264" || stream.Width < 1 || stream.Height < 1 || stream.Width > 1920 || stream.Height > 1920 || int64(stream.Width)*int64(stream.Height) > 1920*1080 {
				return VideoDescriptor{}, ErrVideoInvalid
			}
			parts := strings.Split(stream.FPS, "/")
			if len(parts) != 2 {
				return VideoDescriptor{}, ErrVideoInvalid
			}
			n, e1 := strconv.ParseFloat(parts[0], 64)
			d, e2 := strconv.ParseFloat(parts[1], 64)
			if e1 != nil || e2 != nil || math.IsNaN(n) || math.IsNaN(d) || math.IsInf(n, 0) || math.IsInf(d, 0) || d <= 0 || n/d <= 0 || n/d > 60 {
				return VideoDescriptor{}, ErrVideoInvalid
			}
			descriptor.Width = stream.Width
			descriptor.Height = stream.Height
		case "audio":
			audios++
			if stream.Codec != "aac" {
				return VideoDescriptor{}, ErrVideoInvalid
			}
		default:
			return VideoDescriptor{}, ErrVideoInvalid
		}
	}
	if videos != 1 || audios > 1 {
		return VideoDescriptor{}, ErrVideoInvalid
	}
	decodeCtx, stop := context.WithTimeout(ctx, 25*time.Second)
	defer stop()
	decodeErr := &boundedVideoOutput{limit: 64 * 1024}
	decodeProgress := &boundedVideoOutput{limit: 64 * 1024}
	decoder := exec.CommandContext(decodeCtx, s.FFmpegPath, "-hide_banner", "-v", "error", "-nostats", "-progress", "pipe:1", "-max_alloc", "33554432", "-protocol_whitelist", "file,pipe", "-enable_drefs", "0", "-use_absolute_path", "0", "-max_streams", "2", "-xerror", "-err_detect", "explode", "-threads", "1", "-i", name, "-map", "0:v:0", "-map", "0:a?", "-f", "null", "-")
	decoder.Stdout = decodeProgress
	decoder.Stderr = decodeErr
	if decoder.Run() != nil {
		return VideoDescriptor{}, ErrVideoInvalid
	}
	// Verify decoded duration/frame work as well as declared container duration.
	// No -t truncation: every sample must decode before the final end marker.
	var frames, decodedUS int64
	ended := false
	for _, line := range strings.Split(decodeProgress.String(), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "frame":
			n, e := strconv.ParseInt(parts[1], 10, 64)
			if e != nil || n < 0 || n > 1800 {
				return VideoDescriptor{}, ErrVideoInvalid
			}
			if n > frames {
				frames = n
			}
		case "out_time_us":
			n, e := strconv.ParseInt(parts[1], 10, 64)
			if e != nil || n < 0 || n > MaxVideoDurationMS*1000 {
				return VideoDescriptor{}, ErrVideoInvalid
			}
			if n > decodedUS {
				decodedUS = n
			}
		case "progress":
			if parts[1] == "end" {
				ended = true
			}
		}
	}
	if !ended || frames < 1 || decodedUS < 1 || absVideo(decodedUS/1000-descriptor.DurationMS) > 250 {
		return VideoDescriptor{}, ErrVideoInvalid
	}
	return descriptor, nil
}
func VideoRuntimeFromEnvironment(getenv func(string) string) (VideoRuntime, error) {
	if getenv == nil {
		return VideoRuntime{}, ErrVideoUnavailable
	}
	enabled := strings.TrimSpace(getenv("BIRDTIE_VIDEO_ENABLED"))
	if enabled == "" || enabled == "0" {
		return VideoRuntime{}, nil
	}
	if enabled != "1" {
		return VideoRuntime{}, ErrVideoUnavailable
	}
	dev := getenv("BIRDTIE_ENV") == "development"
	storageURL := strings.TrimSpace(getenv("BIRDTIE_VIDEO_STORAGE_URL"))
	scannerURL := strings.TrimSpace(getenv("BIRDTIE_VIDEO_SCANNER_URL"))
	origin := strings.TrimSpace(getenv("BIRDTIE_VIDEO_UPLOAD_ORIGIN"))
	label, region := strings.TrimSpace(getenv("BIRDTIE_VIDEO_PROVIDER_LABEL")), strings.TrimSpace(getenv("BIRDTIE_VIDEO_REGION"))
	st, sc := getenv("BIRDTIE_VIDEO_STORAGE_TOKEN"), getenv("BIRDTIE_VIDEO_SCANNER_TOKEN")
	probe, decoder := getenv("BIRDTIE_VIDEO_FFPROBE_PATH"), getenv("BIRDTIE_VIDEO_FFMPEG_PATH")
	for _, raw := range []string{storageURL, scannerURL, origin} {
		if _, e := videoServiceURL(raw, dev); e != nil {
			return VideoRuntime{}, e
		}
	}
	u, _ := videoServiceURL(origin, dev)
	if u.Path != "" && u.Path != "/" {
		return VideoRuntime{}, ErrVideoUnavailable
	}
	if label == "" || region == "" || len(label) > 120 || len(region) > 120 || st == "" || sc == "" || strings.ContainsAny(st+sc, "\r\n") || !filepath.IsAbs(probe) || !filepath.IsAbs(decoder) {
		return VideoRuntime{}, ErrVideoUnavailable
	}
	for _, file := range []string{probe, decoder} {
		info, e := os.Stat(file)
		if e != nil || info.IsDir() {
			return VideoRuntime{}, ErrVideoUnavailable
		}
	}
	return VideoRuntime{Storage: &HTTPVideoStorage{BaseURL: storageURL, Token: st, UploadOrigin: origin, Development: dev}, Scanner: &HTTPVideoScanner{URL: scannerURL, Token: sc, Development: dev}, Inspector: &FFmpegVideoInspector{FFprobePath: probe, FFmpegPath: decoder}, ProviderLabel: label, Region: region, UploadOrigin: origin}, nil
}
