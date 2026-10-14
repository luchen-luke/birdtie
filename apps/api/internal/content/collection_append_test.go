package content

import (
	"strings"
	"testing"
)

const appendContractBody = `{"cityId":"aberdeen-gb","expectedRevision":1,"operationId":"10000000-0000-4000-8000-000000000001","entries":[{"placeId":"20000000-0000-4000-8000-000000000002","note":"","locationVisibility":"hidden"}]}`

func TestCollectionAppendClosedOriginalOperationAndNoPrivateNoteCopy(t *testing.T) {
	in, e := DecodeCollectionAppend([]byte(appendContractBody))
	if e != nil || in.CityID != "aberdeen-gb" {
		t.Fatal("original city slug rejected", e)
	}
	for _, raw := range []string{strings.Replace(appendContractBody, `"note":""`, `"note":"private bookmark note"`, 1), strings.Replace(appendContractBody, `"hidden"`, `"place"`, 1), strings.Replace(appendContractBody, `"cityId":"aberdeen-gb"`, `"cityId":"aberdeen-gb","cityId":"other"`, 1), strings.Replace(appendContractBody, `"placeId":`, `"owner":"other","placeId":`, 1), strings.Replace(appendContractBody, `"expectedRevision":1`, `"expectedRevision":9000000000000000`, 1)} {
		if _, e := DecodeCollectionAppend([]byte(raw)); e == nil {
			t.Fatal("unknown/duplicate/private/new visibility accepted")
		}
	}
	first := CollectionAppendDigest("journey", "30000000-0000-4000-8000-000000000003", in)
	if first == CollectionAppendDigest("theme_map", "30000000-0000-4000-8000-000000000003", in) {
		t.Fatal("operation digest unbound from kind")
	}
	in.ExpectedRevision++
	if first == CollectionAppendDigest("journey", "30000000-0000-4000-8000-000000000003", in) {
		t.Fatal("operation digest unbound from original revision")
	}
}
