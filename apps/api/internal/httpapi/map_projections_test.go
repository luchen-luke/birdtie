package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestMapProjectionHTTPStrictWire(t *testing.T) {
	for _, suffix := range []string{"west=-2.2&south=57.1&east=-2&north=57.3", "west=-2.2&south=57.1&east=-2&north=57.3&ownerId=fake", "west=-2.2&west=-2&south=57.1&east=-2&north=57.3", "west=NaN&south=57.1&east=-2&north=57.3", "west=-2.2&south=57.1&east=-2&north=57.3&layers=PRIVATE"} {
		r := httptest.NewRequest("GET", "http://localhost/v1/cities/test/map-layers?"+suffix, nil)
		r.SetPathValue("cityID", "test-city")
		_, e := mapProjectionQuery(r, false)
		if (e == nil) != (suffix == "west=-2.2&south=57.1&east=-2&north=57.3") {
			t.Fatal(suffix, e)
		}
	}
}
