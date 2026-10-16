// Derived from Birdtie cmd/legacy-import private input/target guards.
// Explicit native draft adaptation into one guarded native database.
// Default execution validates a protected input and performs no DB access.
package main

import (
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
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacymomentauthor"
	rm "github.com/birdtie/birdtie/apps/api/internal/legacyreferencemedia"
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
	config.ConnConfig.RuntimeParams["application_name"] = "birdtie-legacy-moment-author-restore"
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

type publicSummary struct {
	RunID         string                     `json:"runId"`
	Status        string                     `json:"status"`
	Counts        *legacymomentauthor.Counts `json:"counts,omitempty"`
	Deferred      map[string]int             `json:"deferred,omitempty"`
	CountsKnown   bool                       `json:"countsKnown"`
	Activated     bool                       `json:"activated"`
	SourceWrites  int                        `json:"sourceWrites"`
	MediaImported bool                       `json:"mediaImported"`
}

func writeSummary(out io.Writer, runID, status string, counts legacymomentauthor.Counts, deferred map[string]int) error {
	summary := publicSummary{RunID: runID, Status: status}
	if status == "PREPARED_NOT_IMPORTED" || status == "COMMITTED" {
		summary.Counts, summary.CountsKnown = &counts, true
		summary.Deferred = deferred
	}
	if err := json.NewEncoder(out).Encode(summary); err != nil {
		return postgres.ErrLegacyMigrationUnavailable
	}
	return nil
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("legacy-import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runID := flags.String("run-id", "", "original bounded preservation run")
	source := flags.String("source", "", "protected absolute source file outside repository")
	matched := flags.String("matched", "", "protected authenticated matcher metadata")
	seal := flags.String("seal", "", "protected authenticated media seal")
	sealSHA := flags.String("seal-sha256", "", "exact original authenticated seal digest")
	blobRoot := flags.String("blob-root", "", "protected exact hash-addressed avatar originals")
	manifest := flags.String("manifest", "", "protected absolute manifest file outside repository")
	verify := flags.Bool("verify-blobs", false, "verify exact originals and bounded avatar codec without database access")
	apply := flags.Bool("apply", false, "restore proven read visibility once in an exact native target")
	inspect := flags.String("inspect-run", "", "inspect only an original run's immutable facts")
	validate := flags.Bool("validate", false, "prepare without database access (default)")
	database := flags.String("database", "", "exact native database name")
	databaseOID := flags.Int64("database-oid", 0, "exact native database OID")
	owner := flags.String("database-owner", "", "exact native database owner role")
	marker := flags.String("database-marker", "", "exact protected native target marker")
	sourceOID := flags.Int64("source-database-oid", 0, "forbidden source database OID in this cluster")
	deadline := flags.Duration("deadline", 90*time.Second, "bounded operation deadline")
	if flags.Parse(args) != nil || flags.NArg() != 0 || ctx == nil || ctx.Err() != nil || *deadline < time.Second || *deadline > 2*time.Minute ||
		*apply && (*inspect != "" || *validate || *verify) || *inspect != "" && (*runID != "" || *source != "" || *manifest != "" || *matched != "" || *seal != "" || *sealSHA != "" || *blobRoot != "" || *validate || *verify) {
		return errArguments
	}
	target := postgres.LegacyMigrationTargetIdentity{DatabaseName: *database, DatabaseOID: *databaseOID, Owner: *owner,
		Marker: *marker, ForbiddenSourceDatabaseOID: *sourceOID}
	var sourceRaw, manifestRaw []byte
	var input postgres.LegacyPrivateMediaInput
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
		input.Source, input.Manifest, input.ExpectedSealSHA256 = sourceRaw, manifestRaw, *sealSHA
		if input.Matched, err = readPrivateInput(*matched, repository); err != nil {
			return err
		}
		if input.AuthenticatedSeal, err = readPrivateInput(*seal, repository); err != nil {
			return err
		}
		plan, err := postgres.PrepareLegacyMomentAuthorRestore(*runID, input)
		if err != nil {
			return postgres.ErrLegacyMigrationInvalid
		}
		if *verify {
			if !filepath.IsAbs(*blobRoot) || pathWithin(filepath.Clean(*blobRoot), filepath.Clean(repository)) {
				return errInputFile
			}
			checked, e := legacymomentauthor.Build(*runID, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
			if e != nil {
				return postgres.ErrLegacyMigrationInvalid
			}
			bounded, cancel := context.WithTimeout(ctx, *deadline)
			defer cancel()
			count, total := 0, 0
			for _, author := range checked.Authors {
				if author.Avatar == nil {
					continue
				}
				thumb, e := postgres.BuildLegacyMomentAuthorAvatar(bounded, *author.Avatar, func(ctx context.Context, a rm.Asset) (io.ReadCloser, error) {
					return openPrivateBlob(*blobRoot, legacymedia.Record{InputSHA256: a.InputSHA256, InputBytes: a.InputBytes})
				})
				if e != nil {
					return e
				}
				count++
				total += len(thumb.Bytes)
				clear(thumb.Bytes)
			}
			return json.NewEncoder(out).Encode(map[string]any{"status": "VERIFIED_NOT_IMPORTED", "avatars": count, "derivedBytes": total, "databaseAccessed": false, "sourceWrites": 0})
		}
		if !*apply {
			return writeSummary(out, *runID, plan.Status, plan.Counts, plan.Deferred)
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
	var result postgres.LegacyMomentAuthorRestoreResult
	if *inspect != "" {
		result, err = importer.InspectLegacyMomentAuthorRestore(ctx, *runID)
	} else {
		result, err = importer.ApplyLegacyMomentAuthorRestore(ctx, *runID, input, func(ctx context.Context, a rm.Asset) (io.ReadCloser, error) {
			repository := getenv("BIRDTIE_REPOSITORY_ROOT")
			if repository == "" && runtime.GOOS == "windows" {
				repository = `D:\Project\birdtie`
			}
			if !filepath.IsAbs(*blobRoot) || !filepath.IsAbs(repository) || pathWithin(*blobRoot, repository) {
				return nil, errInputFile
			}
			return openPrivateBlob(*blobRoot, legacymedia.Record{InputSHA256: a.InputSHA256, InputBytes: a.InputBytes})
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
