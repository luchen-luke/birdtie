package postgres

import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	lr "github.com/birdtie/birdtie/apps/api/internal/locationreference"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	"testing"
)

func TestExternalLocationReferenceNativeWithoutCityAndIndependentRetirement(t *testing.T) {
	f := savedPageNative(t)
	id, kind, obj, _ := lr.OSMIdentity("node/12345")
	f.exec(t, `INSERT INTO location_references(id,provider,object_kind,object_id)VALUES($1,'openstreetmap',$2,$3)`, id, kind, obj)
	f.exec(t, `INSERT INTO location_reference_observations(reference_id,version,name,address,region,country_code,source_url,attribution,coordinate_system,latitude,longitude,observed_at,valid_until)VALUES($1,repeat('a',64),'合成未运营区域地点','','','GB','https://www.openstreetmap.org/node/12345','© OpenStreetMap contributors · ODbL 1.0','wgs84',51.5,-0.1,clock_timestamp(),clock_timestamp()+interval '1 hour')`, id)
	links := []content.MomentLocationInput{{ID: f.id(t), Kind: "REFERENCE", ReferenceID: id, Precision: "place"}}
	in := content.MomentInput{Body: "无需标题或运营城市的合成正文166", TimePrecision: "unknown", LocationPrecision: "none", Locations: &links}
	draft, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "CREATE", Input: &in})
	published, _ := mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: draft.MomentID, ExpectedRevision: draft.AppliedRevision, Input: &in})
	receipt, e := f.store.CaptureMomentLocations(f.ctx, content.PublicMomentAccess{}, published.MomentID, false)
	if e != nil {
		t.Fatal("reference sidecar", e)
	}
	side, e := f.store.FinalizeMomentLocations(f.ctx, content.PublicMomentAccess{}, receipt)
	if e != nil {
		t.Fatal(e)
	}
	items := side.Items.([]content.PublicMomentLocation)
	if len(items) != 1 || items[0].ReferenceID != id || items[0].PlaceID != "" || items[0].CityID != "" || items[0].Anchor == nil || items[0].Anchor.ReferenceID != id {
		t.Fatal("reference disguised as native place/city")
	}
	pins := func(refs bool) int {
		t.Helper()
		q := mp.Query{CityID: "aberdeen-gb", West: -180, South: -90, East: 180, North: 90, Locations: true, MomentMedia: true, References: refs}
		r, e := f.store.ReadMapLayers(f.ctx, mp.Access{}, q)
		if e != nil {
			t.Fatal("reference map", e)
		}
		count := 0
		for _, i := range r.View.Items {
			if i.Kind == "MOMENT" && i.ID == published.MomentID {
				count++
				if i.Anchor.ReferenceID != id || i.Detail.ID != published.MomentID || i.PinIdentity != links[0].ID {
					t.Fatal("wrong reference pin")
				}
			}
		}
		return count
	}
	if pins(true) != 1 || pins(false) != 0 {
		t.Fatal("v6 pin missing or leaked into v5")
	}
	f.exec(t, `UPDATE location_references SET status='RETIRED' WHERE id=$1`, id)
	if _, e = f.store.FinalizeMomentLocations(f.ctx, content.PublicMomentAccess{}, receipt); e == nil {
		t.Fatal("retired source reused")
	}
	if pins(true) != 0 {
		t.Fatal("retired reference pin")
	}
	body, e := f.store.GetPublicMoment(f.ctx, content.PublicMomentAccess{}, published.MomentID)
	if e != nil || body.Body != in.Body {
		t.Fatal("reference retirement hid body", e)
	}
	in.Body = "位置失效仍可继续编辑"
	mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: published.MomentID, ExpectedRevision: published.AppliedRevision, Input: &in})
}
