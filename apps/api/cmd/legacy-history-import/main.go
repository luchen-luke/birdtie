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
	config.ConnConfig.RuntimeParams["application_name"] = "birdtie-legacy-context-import"
	config.MaxConns, config.MinConns = 1, 0
	return config, nil
}

func run(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("legacy-history-import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	run := flags.String("run-id", "", "original run")
	inspect := flags.String("inspect-run", "", "original history receipt")
	apply := flags.Bool("apply", false, "apply only exact owned target")
	database := flags.String("database", "", "exact database")
	oid := flags.Int64("database-oid", 0, "exact database OID")
	owner := flags.String("database-owner", "", "exact role")
	marker := flags.String("database-marker", "", "exact marker")
	sourceOID := flags.Int64("source-database-oid", 0, "forbidden source OID")
	deadline := flags.Duration("deadline", 90*time.Second, "bounded deadline")
	names := []string{"source", "manifest", "social-source", "social-manifest", "history-source", "history-manifest"}
	paths := make([]*string, 6)
	for i, k := range names {
		paths[i] = flags.String(k, "", "protected absolute input")
	}
	if flags.Parse(args) != nil || flags.NArg() != 0 || ctx == nil || ctx.Err() != nil || *deadline < time.Second || *deadline > 2*time.Minute || *inspect != "" && (*run != "" || *apply) {
		return errArguments
	}
	inputs := make([][]byte, 6)
	if *inspect != "" {
		*run = *inspect
		for _, p := range paths {
			if *p != "" {
				return errArguments
			}
		}
	} else {
		if !legacymigration.ValidRunID(*run) {
			return errArguments
		}
		repo := getenv("BIRDTIE_REPOSITORY_ROOT")
		if repo == "" && runtime.GOOS == "windows" {
			repo = `D:\Project\birdtie`
		}
		seen := map[string]bool{}
		for i, p := range paths {
			if *p == "" || seen[filepath.Clean(*p)] {
				return errArguments
			}
			seen[filepath.Clean(*p)] = true
			raw, e := readPrivateInput(*p, repo)
			if e != nil {
				return e
			}
			inputs[i] = raw
		}
	}
	var result postgres.LegacySocialHistoryResult
	var err error
	if !*apply && *inspect == "" {
		result, err = postgres.PrepareLegacySocialHistory(*run, inputs[0], inputs[1], inputs[2], inputs[3], inputs[4], inputs[5])
	} else {
		target := postgres.LegacyMigrationTargetIdentity{DatabaseName: *database, DatabaseOID: *oid, Owner: *owner, Marker: *marker, ForbiddenSourceDatabaseOID: *sourceOID}
		if !legacymigration.ValidRunID(*run) || postgres.ValidateLegacyMigrationTarget(target) != nil {
			return errConfiguration
		}
		config, e := legacyImportConfig(getenv("BIRDTIE_DATABASE_URL"), target, getenv)
		if e != nil {
			return e
		}
		ctx, cancel := context.WithTimeout(ctx, *deadline)
		defer cancel()
		pool, e := pgxpool.NewWithConfig(ctx, config)
		if e != nil {
			return postgres.ErrLegacyMigrationUnavailable
		}
		defer pool.Close()
		importer, e := postgres.NewLegacyMigrationImporter(pool, target)
		if e != nil {
			return e
		}
		if *inspect != "" {
			result, err = importer.InspectLegacySocialHistory(ctx, *run)
		} else {
			result, err = importer.ApplyLegacySocialHistory(ctx, *run, inputs[0], inputs[1], inputs[2], inputs[3], inputs[4], inputs[5])
		}
	}
	if err != nil && !errors.Is(err, postgres.ErrLegacyMigrationUnknown) && !errors.Is(err, postgres.ErrLegacyMigrationNotFound) {
		return err
	}
	safe := map[string]any{"runId": *run, "status": result.Status, "countsKnown": false, "sourceWrites": 0, "modelCalls": 0, "pushEffects": 0, "newNotificationEffects": 0, "agentActivated": false}
	if err == nil {
		safe["countsKnown"] = true
		safe["counts"] = result.Counts
	}
	if json.NewEncoder(out).Encode(safe) != nil {
		return postgres.ErrLegacyMigrationUnavailable
	}
	return err
}
func main() {
	if e := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	}
}
