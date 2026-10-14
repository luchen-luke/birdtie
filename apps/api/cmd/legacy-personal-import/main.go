// Derived from Birdtie cmd/legacy-import private input/target guards.
// Explicit owner/archive preservation into one guarded native database.
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

	"github.com/birdtie/birdtie/apps/api/internal/legacycollections"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
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
	config.ConnConfig.RuntimeParams["application_name"] = "birdtie-legacy-personal-import"
	config.MaxConns, config.MinConns = 1, 0
	return config, nil
}

type publicSummary struct {
	RunID         string                    `json:"runId"`
	Status        string                    `json:"status"`
	Counts        *legacycollections.Counts `json:"counts,omitempty"`
	CountsKnown   bool                      `json:"countsKnown"`
	Activated     bool                      `json:"activated"`
	SourceWrites  int                       `json:"sourceWrites"`
	MediaImported bool                      `json:"mediaImported"`
}

func writeSummary(out io.Writer, runID, status string, counts legacycollections.Counts) error {
	summary := publicSummary{RunID: runID, Status: status}
	if status == "PREPARED_NOT_IMPORTED" || status == "COMMITTED" {
		summary.Counts, summary.CountsKnown = &counts, true
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
	manifest := flags.String("manifest", "", "protected absolute manifest file outside repository")
	apply := flags.Bool("apply", false, "apply the original run once to an exact native target")
	inspect := flags.String("inspect-run", "", "inspect only an original run's immutable facts")
	validate := flags.Bool("validate", false, "prepare without database access (default)")
	database := flags.String("database", "", "exact native database name")
	databaseOID := flags.Int64("database-oid", 0, "exact native database OID")
	owner := flags.String("database-owner", "", "exact native database owner role")
	marker := flags.String("database-marker", "", "exact protected native target marker")
	sourceOID := flags.Int64("source-database-oid", 0, "forbidden source database OID in this cluster")
	deadline := flags.Duration("deadline", 90*time.Second, "bounded operation deadline")
	if flags.Parse(args) != nil || flags.NArg() != 0 || ctx == nil || ctx.Err() != nil || *deadline < time.Second || *deadline > 2*time.Minute ||
		*apply && (*inspect != "" || *validate) || *inspect != "" && (*runID != "" || *source != "" || *manifest != "" || *validate) {
		return errArguments
	}
	target := postgres.LegacyMigrationTargetIdentity{DatabaseName: *database, DatabaseOID: *databaseOID, Owner: *owner,
		Marker: *marker, ForbiddenSourceDatabaseOID: *sourceOID}
	var sourceRaw, manifestRaw []byte
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
		plan, err := legacycollections.BuildPlan(*runID, sourceRaw, manifestRaw)
		if err != nil {
			return postgres.ErrLegacyMigrationInvalid
		}
		if !*apply {
			counts := plan.Counts
			return writeSummary(out, *runID, "PREPARED_NOT_IMPORTED", counts)
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
	var result postgres.LegacyPersonalArchiveResult
	if *inspect != "" {
		result, err = importer.InspectPersonalArchives(ctx, *runID)
	} else {
		result, err = importer.ApplyPersonalArchives(ctx, *runID, sourceRaw, manifestRaw)
	}
	if err != nil {
		if errors.Is(err, postgres.ErrLegacyMigrationUnknown) || errors.Is(err, postgres.ErrLegacyMigrationNotFound) {
			if outputErr := writeSummary(out, *runID, result.Status, result.Counts); outputErr != nil {
				return outputErr
			}
		}
		return err
	}
	return writeSummary(out, result.RunID, result.Status, result.Counts)
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); err != nil {
		// Every returned error above is a fixed code. Never print flag values,
		// input paths, raw PG errors, source text, credential or digest values.
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
