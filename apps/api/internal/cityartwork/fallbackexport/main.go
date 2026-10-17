// Command fallbackexport builds the two explicitly reviewed offline display
// assets using the same derivative as the server. It never reads a database.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/birdtie/birdtie/apps/api/internal/cityartwork"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: fallbackexport protected-source-directory selected-destination-assets-directory")
		os.Exit(2)
	}
	var receipt []map[string]any
	for _, x := range []struct{ city, source, name string }{
		{"civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00", "d2988c88b86453e1ead0c637ffeb85abdcfd89c9ce3894d12351a609b4429988", "qingdao-d2988c88.png"},
		{"civu-geo-bbffd2af-68b4-49ee-bb01-5e5a7673aa9b", "69b9d3d653f3b7f64fd1423c54e18d73cb90f1186a824bb1cfe1319a835a4218", "jinan-69b9d3d6.png"},
	} {
		raw, e := os.ReadFile(filepath.Join(os.Args[1], x.source+".blob"))
		if e != nil {
			fail(e)
		}
		sum := sha256.Sum256([]byte(x.source + ":" + x.source + ":souvenir324-v1"))
		version := hex.EncodeToString(sum[:])
		v, e := cityartwork.Build(context.Background(), x.city, version, x.source, raw)
		clear(raw)
		if e != nil {
			fail(e)
		}
		path := filepath.Join(os.Args[2], x.name)
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			fail(e)
		}
		_, e = f.Write(v.Bytes)
		closed := f.Close()
		if e != nil {
			fail(e)
		}
		if closed != nil {
			fail(closed)
		}
		receipt = append(receipt, map[string]any{"cityId": x.city, "sourceVersion": x.source, "sourceSHA256": x.source, "asset": path, "projectionVersion": version, "derivative": v.Descriptor})
	}
	if e := json.NewEncoder(os.Stdout).Encode(receipt); e != nil {
		fail(e)
	}
}
func fail(e error) { fmt.Fprintln(os.Stderr, e); os.Exit(1) }
