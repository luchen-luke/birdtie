package legacymap

import (
	"encoding/json"
	"testing"
)

func TestCoordinateProofRequiresExactOriginAndRejectsConflict(t *testing.T) {
	p := row{"lat": json.Number("57.15"), "lng": json.Number("-2.1"), "source": "osm"}
	r := row{"provider": "openstreetmap", "external_id": "node/123", "source_payload": row{"promotedFrom": "local-civu-catalog", "sourcePlaceId": "12345678-1234-4234-8234-123456789012"}}
	proof, lat, lng := coordinateProof(p, []row{r})
	if proof != "OSM_PROVIDER_IDENTITY_PROMOTION_V1" || lat == nil || *lat != 57.15 || lng == nil || *lng != -2.1 {
		t.Fatal("exact OSM promotion not retained")
	}
	r["external_id"] = "unknown/123"
	if _, lat, _ = coordinateProof(p, []row{r}); lat != nil {
		t.Fatal("unknown provider identity gained coordinate authority")
	}
	r["external_id"] = "node/123"
	conflict := row{"source_payload": row{"storedCoordinateSystem": "GCJ-02"}}
	if _, lat, _ = coordinateProof(p, []row{r, conflict}); lat != nil {
		t.Fatal("conflicting CRS accepted")
	}
	if _, lat, _ = coordinateProof(p, nil); lat != nil {
		t.Fatal("numeric range alone inferred WGS84")
	}
}
