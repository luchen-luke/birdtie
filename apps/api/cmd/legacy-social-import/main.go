// Derived from Birdtie cmd/legacy-import private input/target guards.
// Explicit original social facts into one guarded native database.
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
	config.ConnConfig.RuntimeParams["application_name"] = "birdtie-legacy-native-import"
	config.MaxConns, config.MinConns = 1, 0
	return config, nil
}

type publicSummary struct {
	RunID         string                       `json:"runId"`
	Status        string                       `json:"status"`
	Counts        *postgres.LegacyNativeCounts `json:"counts,omitempty"`
	Deferred      map[string]int               `json:"deferred,omitempty"`
	CountsKnown   bool                         `json:"countsKnown"`
	Activated     bool                         `json:"activated"`
	SourceWrites  int                          `json:"sourceWrites"`
	MediaImported bool                         `json:"mediaImported"`
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("legacy-social-import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runID := flags.String("run-id", "", "original146 run UUID")
	source := flags.String("source", "", "protected original source")
	manifest := flags.String("manifest", "", "protected original manifest")
	socialSource := flags.String("social-source", "", "sealed8-table source")
	socialManifest := flags.String("social-manifest", "", "sealed8-table manifest")
	matcher := flags.String("image-match", "", "exact authenticated image match")
	seal := flags.String("image-seal", "", "independent authenticated extraction seal")
	sealSHA := flags.String("image-seal-sha256", "", "operator pinned extraction seal digest")
	image := flags.String("image-content", "", "exact source blob")
	apply := flags.Bool("apply", false, "apply atomic native import")
	inspect := flags.String("inspect-run", "", "inspect immutable original fact")
	deadline := flags.Duration("deadline", 2*time.Minute, "bounded operation")
	name := flags.String("target-database", "", "native database")
	oid := flags.Int64("target-database-oid", 0, "native OID")
	owner := flags.String("target-owner", "", "native owner")
	marker := flags.String("target-marker", "", "native guard marker")
	forbidden := flags.Int64("source-database-oid", 0, "forbidden source OID")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *deadline < time.Second || *deadline > 5*time.Minute {
		return errArguments
	}
	if *inspect != "" {
		*runID = *inspect
	}
	if !legacymigration.ValidRunID(*runID) {
		return errArguments
	}
	ctx, cancel := context.WithTimeout(ctx, *deadline)
	defer cancel()
	var sourceRaw, manifestRaw, socialRaw, socialManifestRaw []byte
	var input postgres.LegacySocialImageInput
	var e error
	if *inspect == "" {
		repo := getenv("BIRDTIE_REPOSITORY_ROOT")
		if repo == "" && runtime.GOOS == "windows" {
			repo = `D:\Project\birdtie`
		}
		read := func(path string) []byte {
			if e != nil {
				return nil
			}
			var b []byte
			b, e = readPrivateInput(path, repo)
			return b
		}
		sourceRaw = read(*source)
		manifestRaw = read(*manifest)
		socialRaw = read(*socialSource)
		socialManifestRaw = read(*socialManifest)
		if *matcher != "" || *seal != "" || *image != "" || *sealSHA != "" {
			input.Matcher = read(*matcher)
			input.Seal = read(*seal)
			input.Content = read(*image)
			input.ExpectedSealSHA256 = *sealSHA
		}
		if e != nil {
			return errInputFile
		}
		prepared, e := postgres.PrepareLegacySocial(ctx, *runID, sourceRaw, manifestRaw, socialRaw, socialManifestRaw, input)
		if e != nil {
			return e
		}
		if !*apply {
			return json.NewEncoder(out).Encode(prepared)
		}
	}
	target := postgres.LegacyMigrationTargetIdentity{DatabaseName: *name, DatabaseOID: *oid, Owner: *owner, Marker: *marker, ForbiddenSourceDatabaseOID: *forbidden}
	if postgres.ValidateLegacyMigrationTarget(target) != nil {
		return errConfiguration
	}
	config, e := legacyImportConfig(getenv("BIRDTIE_DATABASE_URL"), target, getenv)
	if e != nil {
		return e
	}
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		return postgres.ErrLegacyMigrationUnavailable
	}
	defer pool.Close()
	importer, e := postgres.NewLegacyMigrationImporter(pool, target)
	if e != nil {
		return e
	}
	var result postgres.LegacySocialResult
	if *inspect != "" {
		result, e = importer.InspectLegacySocial(ctx, *runID)
	} else {
		result, e = importer.ApplyLegacySocial(ctx, *runID, sourceRaw, manifestRaw, socialRaw, socialManifestRaw, input)
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(result)
}
func main() {
	if e := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	}
}
