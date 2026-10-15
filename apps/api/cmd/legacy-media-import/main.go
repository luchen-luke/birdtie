// Derived from Birdtie cmd/legacy-import private input/target guards.
// Explicit authenticated historical JPEG import into one guarded native database.
// Default execution validates a protected input and performs no DB access.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/legacyimagerecovery"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errArguments = errors.New("legacy_import_arguments_invalid")
var errInputFile = errors.New("legacy_import_private_input_rejected")
var errConfiguration = errors.New("legacy_import_target_configuration_rejected")

func pathWithin(path, root string) bool {
	if runtime.GOOS == "windows" {
		path, root = strings.ToLower(path), strings.ToLower(root)
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func reparseOrLink(info os.FileInfo) bool {
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	// Go's Windows FileInfo.Sys supplies Win32FileAttributeData. Reflection
	// keeps the CLI portable without copying credentials or requiring a native
	// Windows-only shim; reject every reparse tag including directory junctions.
	v := reflect.ValueOf(info.Sys())
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return true
	}
	attributes := v.Elem().FieldByName("FileAttributes")
	return !attributes.IsValid() || !attributes.CanUint() || attributes.Uint()&0x400 != 0
}

func inputPathState(path string) (os.FileInfo, error) {
	var final os.FileInfo
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || reparseOrLink(info) || (current != path && !info.IsDir()) {
			return nil, errInputFile
		}
		if current == path {
			final = info
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return final, nil
}

func readPrivateInput(path, repositoryRoot string) ([]byte, error) {
	if !filepath.IsAbs(path) || !filepath.IsAbs(repositoryRoot) || strings.ContainsRune(path, 0) {
		return nil, errInputFile
	}
	path, repositoryRoot = filepath.Clean(path), filepath.Clean(repositoryRoot)
	if pathWithin(path, repositoryRoot) || runtime.GOOS == "windows" && pathWithin(path, `D:\Project\birdtie`) {
		return nil, errInputFile
	}
	before, err := inputPathState(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > postgres.LegacyMigrationMaxInputBytes {
		return nil, errInputFile
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errInputFile
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || reparseOrLink(opened) || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Size() != before.Size() {
		return nil, errInputFile
	}
	raw, err := io.ReadAll(io.LimitReader(f, postgres.LegacyMigrationMaxInputBytes+1))
	after, pathErr := inputPathState(path)
	if err != nil || pathErr != nil || len(raw) != int(before.Size()) || len(raw) > postgres.LegacyMigrationMaxInputBytes ||
		!os.SameFile(opened, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, errInputFile
	}
	// Read-only handles preserve the original CurrentUser DACL and file bytes.
	return raw, nil
}

func legacyImportConfig(raw string, target postgres.LegacyMigrationTargetIdentity, getenv func(string) string) (*pgxpool.Config, error) {
	if postgres.ValidateLegacyMigrationTarget(target) != nil {
		return nil, errConfiguration
	}
	for _, key := range []string{"PGHOST", "PGHOSTADDR", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSERVICE", "PGSERVICEFILE", "PGOPTIONS", "PGSSLMODE", "PGSSLROOTCERT", "PGSSLKEY", "PGSSLCERT", "PGTARGETSESSIONATTRS", "PGAPPNAME"} {
		if getenv(key) != "" {
			return nil, errConfiguration
		}
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.User == nil || u.Fragment != "" || u.Opaque != "" || u.Path != "/"+target.DatabaseName {
		return nil, errConfiguration
	}
	config, err := pgxpool.ParseConfig(raw)
	if err != nil || config.ConnConfig.Database != target.DatabaseName || config.ConnConfig.User != target.Owner {
		return nil, errConfiguration
	}
	for key := range config.ConnConfig.RuntimeParams {
		if key != "application_name" {
			return nil, errConfiguration
		}
	}
	host := config.ConnConfig.Host
	ip := net.ParseIP(host)
	local := strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback() || strings.HasPrefix(host, "/")
	if !local && (config.ConnConfig.TLSConfig == nil || config.ConnConfig.TLSConfig.InsecureSkipVerify || len(config.ConnConfig.Fallbacks) != 0) {
		return nil, errConfiguration
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = map[string]string{}
	}
	config.ConnConfig.RuntimeParams["search_path"] = "public"
	config.ConnConfig.RuntimeParams["application_name"] = "birdtie-legacy-media-import"
	config.MaxConns, config.MinConns = 1, 0
	return config, nil
}

type privateBlobReader struct {
	file   *os.File
	path   string
	before os.FileInfo
}

func (r *privateBlobReader) Read(b []byte) (int, error) { return r.file.Read(b) }
func (r *privateBlobReader) Close() error {
	closeErr := r.file.Close()
	after, e := inputPathState(r.path)
	if closeErr != nil || e != nil || !os.SameFile(r.before, after) || after.Size() != r.before.Size() || !after.ModTime().Equal(r.before.ModTime()) {
		return errInputFile
	}
	return nil
}
func openPrivateBlob(root string, r legacymedia.Record) (io.ReadCloser, error) {
	if !legacymedia.ValidSHA(r.InputSHA256) || r.InputBytes < 1 || r.InputBytes > legacymedia.MaxInputBytes {
		return nil, errInputFile
	}
	path := filepath.Join(root, r.InputSHA256+".blob")
	before, e := inputPathState(path)
	if e != nil || !before.Mode().IsRegular() || before.Size() != r.InputBytes {
		return nil, errInputFile
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errInputFile
	}
	opened, e := f.Stat()
	if e != nil || reparseOrLink(opened) || !os.SameFile(before, opened) || opened.Size() != before.Size() {
		f.Close()
		return nil, errInputFile
	}
	return &privateBlobReader{f, path, before}, nil
}

func verifyPrivateBlob(ctx context.Context, root string, record legacymedia.Record) string {
	r, e := openPrivateBlob(root, record)
	if e != nil {
		return "INPUT_FILE_GUARD"
	}
	raw, e := io.ReadAll(io.LimitReader(r, record.InputBytes+1))
	closed := r.Close()
	defer clear(raw)
	if e != nil || closed != nil {
		return "INPUT_READ"
	}
	if int64(len(raw)) != record.InputBytes || legacymedia.Hash(raw) != record.InputSHA256 {
		return "INPUT_HASH_OR_SIZE"
	}
	out, mime, e := (&media.LocalImageDerivativeBuilder{}).BuildLegacyPrivateJPEG(ctx, bytes.NewReader(raw))
	if e != nil {
		return media.ImageDerivativeFailureStage(e)
	}
	defer out.Close()
	derived, e := io.ReadAll(io.LimitReader(out, media.MaxImageOutputBytes+1))
	defer clear(derived)
	if e != nil || mime != "image/jpeg" || len(derived) < 1 || len(derived) > media.MaxImageOutputBytes {
		return "DERIVATIVE_OUTPUT"
	}
	return ""
}

func verifyPrivateBlobs(ctx context.Context, out io.Writer, plan legacymedia.Plan, root, repository string, deadline time.Duration) error {
	if !filepath.IsAbs(root) || !filepath.IsAbs(repository) || pathWithin(filepath.Clean(root), filepath.Clean(repository)) || runtime.GOOS == "windows" && pathWithin(filepath.Clean(root), `D:\Project\birdtie`) {
		return errInputFile
	}
	info, e := inputPathState(filepath.Clean(root))
	if e != nil || !info.IsDir() {
		return errInputFile
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	previous := debug.SetMemoryLimit(512 * 1024 * 1024)
	defer debug.SetMemoryLimit(previous)
	failures := map[string]int{}
	processed, verified := 0, 0
	for _, record := range plan.Records {
		if ctx.Err() != nil {
			failures["DEADLINE"]++
			break
		}
		stage := verifyPrivateBlob(ctx, root, record)
		processed++
		if stage == "" {
			verified++
		} else {
			failures[stage]++
		}
		if processed%15 == 0 {
			if e = json.NewEncoder(out).Encode(map[string]any{"status": "VERIFYING_NOT_IMPORTED", "processed": processed, "expected": plan.Counts.Images, "verified": verified, "failed": processed - verified, "failureStages": failures, "databaseAccessed": false, "mediaWritten": false}); e != nil {
				return errInputFile
			}
		}
	}
	status := "VERIFIED_NOT_IMPORTED"
	if verified != plan.Counts.Images {
		status = "VERIFICATION_FAILED_NOT_IMPORTED"
	}
	if e = json.NewEncoder(out).Encode(map[string]any{"status": status, "processed": processed, "expected": plan.Counts.Images, "verified": verified, "failed": processed - verified, "unprocessed": plan.Counts.Images - processed, "failureStages": failures, "databaseAccessed": false, "mediaWritten": false, "archivesChanged": false}); e != nil {
		return errInputFile
	}
	if verified != plan.Counts.Images {
		return errors.New("legacy_private_media_verification_failed")
	}
	return nil
}

type publicSummary struct {
	RunID          string              `json:"runId"`
	Status         string              `json:"status"`
	Counts         *legacymedia.Counts `json:"counts,omitempty"`
	Deferred       map[string]int      `json:"deferred,omitempty"`
	CountsKnown    bool                `json:"countsKnown"`
	Activated      bool                `json:"activated"`
	SourceWrites   int                 `json:"sourceWrites"`
	MediaImported  *bool               `json:"mediaImported,omitempty"`
	VideosImported bool                `json:"videosImported"`
}

func writeSummary(out io.Writer, runID, status string, counts legacymedia.Counts, deferred map[string]int) error {
	summary := publicSummary{RunID: runID, Status: status}
	if status == "PREPARED_NOT_IMPORTED" || status == "COMMITTED" {
		imported := status == "COMMITTED"
		summary.MediaImported = &imported
		summary.Counts, summary.CountsKnown = &counts, true
		summary.Deferred = deferred
	}
	if err := json.NewEncoder(out).Encode(summary); err != nil {
		return postgres.ErrLegacyMigrationUnavailable
	}
	return nil
}

type recoveryOptions struct {
	run, source, manifest, matched, seal, sealSHA, blobs, inspect string
	apply, verify                                                 bool
	target                                                        postgres.LegacyMigrationTargetIdentity
	normalizer                                                    legacyimagerecovery.Normalizer
	deadline                                                      time.Duration
}

func writeRecoverySummary(out io.Writer, r postgres.LegacyPrivateImageRecoveryResult) error {
	summary := map[string]any{"runId": r.RunID, "status": r.Status, "protocol": legacyimagerecovery.Protocol, "countsKnown": false, "activated": false, "sourceWrites": 0, "videosImported": false}
	if r.Status == "PREPARED_NOT_IMPORTED" || r.Status == "COMMITTED" {
		summary["countsKnown"] = true
		summary["counts"] = r.Counts
		summary["deferred"] = r.Deferred
		summary["mediaImported"] = r.Status == "COMMITTED"
	}
	if json.NewEncoder(out).Encode(summary) != nil {
		return postgres.ErrLegacyMigrationUnavailable
	}
	return nil
}
func recoveryBlobRoot(root, repository string) error {
	if !filepath.IsAbs(root) || !filepath.IsAbs(repository) || pathWithin(filepath.Clean(root), filepath.Clean(repository)) || runtime.GOOS == "windows" && pathWithin(filepath.Clean(root), `D:\Project\birdtie`) {
		return errInputFile
	}
	info, e := inputPathState(filepath.Clean(root))
	if e != nil || !info.IsDir() {
		return errInputFile
	}
	return nil
}
func verifyRecoveredBlob(ctx context.Context, root string, r legacyimagerecovery.Record, n *legacyimagerecovery.Normalizer) string {
	f, e := openPrivateBlob(root, r.Record)
	if e != nil {
		return "INPUT_FILE_GUARD"
	}
	raw, e := io.ReadAll(io.LimitReader(f, r.InputBytes+1))
	closed := f.Close()
	defer clear(raw)
	if e != nil || closed != nil || int64(len(raw)) != r.InputBytes || legacymedia.Hash(raw) != r.InputSHA256 {
		return "INPUT_HASH_OR_SIZE"
	}
	d, e := n.Normalize(ctx, r, raw)
	defer d.Erase()
	if e != nil {
		return legacyimagerecovery.FailureStage(e)
	}
	return ""
}
func runImageRecovery(ctx context.Context, o recoveryOptions, getenv func(string) string, out io.Writer) error {
	if o.inspect != "" {
		o.run = o.inspect
	}
	if !legacymigration.ValidRunID(o.run) {
		return errArguments
	}
	repository := getenv("BIRDTIE_REPOSITORY_ROOT")
	if repository == "" && runtime.GOOS == "windows" {
		repository = `D:\Project\birdtie`
	}
	var input postgres.LegacyPrivateMediaInput
	var plan legacyimagerecovery.Plan
	var e error
	if o.inspect == "" {
		if o.source == "" || o.manifest == "" || filepath.Clean(o.source) == filepath.Clean(o.manifest) || o.matched == "" || o.seal == "" || !legacymedia.ValidSHA(o.sealSHA) {
			return errArguments
		}
		if input.Source, e = readPrivateInput(o.source, repository); e != nil {
			return e
		}
		if input.Manifest, e = readPrivateInput(o.manifest, repository); e != nil {
			return e
		}
		if input.Matched, e = readPrivateInput(o.matched, repository); e != nil {
			return e
		}
		if input.AuthenticatedSeal, e = readPrivateInput(o.seal, repository); e != nil {
			return e
		}
		input.ExpectedSealSHA256 = o.sealSHA
		plan, e = legacyimagerecovery.Build(o.run, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, o.sealSHA)
		if e != nil {
			return postgres.ErrLegacyMigrationInvalid
		}
		if !o.apply && !o.verify {
			return writeRecoverySummary(out, postgres.LegacyPrivateImageRecoveryResult{RunID: o.run, Status: "PREPARED_NOT_IMPORTED", Counts: plan.Counts, Deferred: plan.Deferred})
		}
		if e = recoveryBlobRoot(o.blobs, repository); e != nil {
			return e
		}
		if plan.Counts.HEIC > 0 && o.normalizer.Validate() != nil {
			return legacyimagerecovery.ErrUnavailable
		}
		if o.verify {
			ctx, cancel := context.WithTimeout(ctx, o.deadline)
			defer cancel()
			previous := debug.SetMemoryLimit(512 * 1024 * 1024)
			defer debug.SetMemoryLimit(previous)
			verified, processed := 0, 0
			failures := map[string]int{}
			for _, r := range plan.Records {
				if ctx.Err() != nil {
					failures["DEADLINE"]++
					break
				}
				stage := verifyRecoveredBlob(ctx, o.blobs, r, &o.normalizer)
				processed++
				if stage == "" {
					verified++
				} else {
					failures[stage]++
				}
				if processed%5 == 0 {
					if json.NewEncoder(out).Encode(map[string]any{"status": "VERIFYING_NOT_IMPORTED", "processed": processed, "verified": verified, "expected": plan.Counts.Images, "failureStages": failures, "databaseAccessed": false, "mediaWritten": false}) != nil {
						return errInputFile
					}
				}
			}
			status := "VERIFIED_NOT_IMPORTED"
			if verified != plan.Counts.Images {
				status = "VERIFICATION_FAILED_NOT_IMPORTED"
			}
			if json.NewEncoder(out).Encode(map[string]any{"status": status, "processed": processed, "verified": verified, "failed": processed - verified, "expected": plan.Counts.Images, "unprocessed": plan.Counts.Images - processed, "failureStages": failures, "databaseAccessed": false, "mediaWritten": false, "archivesChanged": false, "antivirusScan": "NOT_RUN", "privacyRisk": "UNKNOWN"}) != nil {
				return errInputFile
			}
			if verified != plan.Counts.Images {
				return errors.New("legacy_image_recovery_verification_failed")
			}
			return nil
		}
	}
	if postgres.ValidateLegacyMigrationTarget(o.target) != nil {
		return errConfiguration
	}
	config, e := legacyImportConfig(getenv("BIRDTIE_DATABASE_URL"), o.target, getenv)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, o.deadline)
	defer cancel()
	previous := debug.SetMemoryLimit(512 * 1024 * 1024)
	defer debug.SetMemoryLimit(previous)
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		return postgres.ErrLegacyMigrationUnavailable
	}
	defer pool.Close()
	importer, e := postgres.NewLegacyMigrationImporter(pool, o.target)
	if e != nil {
		return e
	}
	var result postgres.LegacyPrivateImageRecoveryResult
	if o.inspect != "" {
		result, e = importer.InspectLegacyPrivateImageRecovery(ctx, o.run)
	} else {
		result, e = importer.ApplyLegacyPrivateImageRecovery(ctx, o.run, input, func(ctx context.Context, r legacyimagerecovery.Record) (io.ReadCloser, error) {
			if ctx.Err() != nil {
				return nil, errInputFile
			}
			return openPrivateBlob(o.blobs, r.Record)
		}, &o.normalizer)
	}
	if e != nil {
		if errors.Is(e, postgres.ErrLegacyMigrationUnknown) || errors.Is(e, postgres.ErrLegacyMigrationNotFound) {
			if x := writeRecoverySummary(out, result); x != nil {
				return x
			}
		}
		return e
	}
	return writeRecoverySummary(out, result)
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("legacy-media-import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runID := flags.String("run-id", "", "original bounded preservation run")
	source := flags.String("source", "", "protected absolute source file outside repository")
	manifest := flags.String("manifest", "", "protected absolute manifest file outside repository")
	apply := flags.Bool("apply", false, "apply the original run once to an exact native target")
	inspect := flags.String("inspect-run", "", "inspect only an original run's immutable facts")
	validate := flags.Bool("validate", false, "prepare without database access (default)")
	database := flags.String("database", "", "exact native database name")
	databaseOID := flags.Int64("database-oid", 0, "exact native database OID")
	owner := flags.String("database-owner", "", "exact native database owner role")
	marker := flags.String("database-marker", "", "exact protected native target marker")
	sourceOID := flags.Int64("source-database-oid", 0, "forbidden source database OID in this cluster")
	matched := flags.String("matched", "", "protected authenticated matcher metadata")
	seal := flags.String("authenticated-seal", "", "protected authenticated original matching seal")
	sealSHA := flags.String("authenticated-seal-sha256", "", "exact seal digest from original controlled receipt")
	blobRoot := flags.String("blob-directory", "", "private original objects named by SHA256 plus .blob, outside repository")
	verify := flags.Bool("verify-blobs", false, "sequentially verify exact original bytes and native JPEG codec without database access or writing media")
	deadline := flags.Duration("deadline", 10*time.Minute, "bounded sequential image import deadline")
	recovery := flags.Bool("metadata-recovery", false, "independent161 authenticated JPEG/HEIC metadata recovery;157 facts unchanged")
	heif := flags.String("heif-convert", "", "absolute libheif heif-convert executable for full static HEIC decode")
	prlimit := flags.String("prlimit", "", "absolute Linux prlimit executable for bounded offline decoding")
	codecTemp := flags.String("codec-temp-root", "", "absolute restricted temporary codec root")
	if flags.Parse(args) != nil || flags.NArg() != 0 || ctx == nil || ctx.Err() != nil || *deadline < time.Second || *deadline > 15*time.Minute ||
		*apply && (*inspect != "" || *validate || *verify) || *inspect != "" && (*runID != "" || *source != "" || *manifest != "" || *matched != "" || *seal != "" || *sealSHA != "" || *blobRoot != "" || *validate || *verify) {
		return errArguments
	}
	target := postgres.LegacyMigrationTargetIdentity{DatabaseName: *database, DatabaseOID: *databaseOID, Owner: *owner,
		Marker: *marker, ForbiddenSourceDatabaseOID: *sourceOID}
	if *recovery {
		return runImageRecovery(ctx, recoveryOptions{run: *runID, source: *source, manifest: *manifest, matched: *matched, seal: *seal, sealSHA: *sealSHA, blobs: *blobRoot, inspect: *inspect, apply: *apply, verify: *verify, target: target, normalizer: legacyimagerecovery.Normalizer{HEIFConvertPath: *heif, PrlimitPath: *prlimit, TempRoot: *codecTemp}, deadline: *deadline}, getenv, out)
	}
	if *heif != "" || *prlimit != "" || *codecTemp != "" {
		return errArguments
	}
	var sourceRaw, manifestRaw, matchedRaw, sealRaw []byte
	var err error
	if *inspect != "" {
		*runID = *inspect
	} else {
		if !legacymigration.ValidRunID(*runID) || *source == "" || *manifest == "" || filepath.Clean(*source) == filepath.Clean(*manifest) {
			return errArguments
		}
		repository := getenv("BIRDTIE_REPOSITORY_ROOT")
		if repository == "" && runtime.GOOS == "windows" {
			repository = `D:\Project\birdtie`
		}
		if sourceRaw, err = readPrivateInput(*source, repository); err != nil {
			return err
		}
		if manifestRaw, err = readPrivateInput(*manifest, repository); err != nil {
			return err
		}
		if *matched == "" || *seal == "" || !legacymedia.ValidSHA(*sealSHA) {
			return errArguments
		}
		if matchedRaw, err = readPrivateInput(*matched, repository); err != nil {
			return err
		}
		if sealRaw, err = readPrivateInput(*seal, repository); err != nil {
			return err
		}
		plan, err := legacymedia.Build(*runID, sourceRaw, manifestRaw, matchedRaw, sealRaw, *sealSHA)
		if err != nil {
			return postgres.ErrLegacyMigrationInvalid
		}
		if !*apply {
			if *verify {
				return verifyPrivateBlobs(ctx, out, plan, *blobRoot, repository, *deadline)
			}
			counts := plan.Counts
			return writeSummary(out, *runID, "PREPARED_NOT_IMPORTED", counts, plan.Deferred)
		}
	}
	if !legacymigration.ValidRunID(*runID) || postgres.ValidateLegacyMigrationTarget(target) != nil {
		return errConfiguration
	}
	config, err := legacyImportConfig(getenv("BIRDTIE_DATABASE_URL"), target, getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, *deadline)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return postgres.ErrLegacyMigrationUnavailable
	}
	defer pool.Close()
	importer, err := postgres.NewLegacyMigrationImporter(pool, target)
	if err != nil {
		return err
	}
	var result postgres.LegacyPrivateMediaResult
	if *inspect != "" {
		result, err = importer.InspectLegacyPrivateMedia(ctx, *runID)
	} else {
		repository := getenv("BIRDTIE_REPOSITORY_ROOT")
		if repository == "" && runtime.GOOS == "windows" {
			repository = `D:\Project\birdtie`
		}
		if !filepath.IsAbs(*blobRoot) || !filepath.IsAbs(repository) || pathWithin(filepath.Clean(*blobRoot), filepath.Clean(repository)) {
			return errInputFile
		}
		if runtime.GOOS == "windows" && pathWithin(filepath.Clean(*blobRoot), `D:\Project\birdtie`) {
			return errInputFile
		}
		info, e := inputPathState(filepath.Clean(*blobRoot))
		if e != nil || !info.IsDir() {
			return errInputFile
		}
		result, err = importer.ApplyLegacyPrivateMedia(ctx, *runID, postgres.LegacyPrivateMediaInput{Source: sourceRaw, Manifest: manifestRaw, Matched: matchedRaw, AuthenticatedSeal: sealRaw, ExpectedSealSHA256: *sealSHA}, func(ctx context.Context, record legacymedia.Record) (io.ReadCloser, error) {
			if ctx.Err() != nil {
				return nil, errInputFile
			}
			return openPrivateBlob(*blobRoot, record)
		})
	}
	if err != nil {
		if errors.Is(err, postgres.ErrLegacyMigrationUnknown) || errors.Is(err, postgres.ErrLegacyMigrationNotFound) {
			if outputErr := writeSummary(out, *runID, result.Status, result.Counts, result.Deferred); outputErr != nil {
				return outputErr
			}
		}
		return err
	}
	return writeSummary(out, result.RunID, result.Status, result.Counts, result.Deferred)
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); err != nil {
		// Every returned error above is a fixed code. Never print flag values,
		// input paths, raw PG errors, source text, credential or digest values.
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
