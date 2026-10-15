package legacyvideo

import (
	"fmt"
	"testing"
)

func TestPrivateVideoFullTimelineAndClosedContainerLimits(t *testing.T) {
	makeProbe := func(duration, codec, rate string) []byte {
		return []byte(fmt.Sprintf(`{"streams":[{"codec_type":"video","codec_name":%q,"pix_fmt":"yuv420p","width":1280,"height":720,"avg_frame_rate":%q,"time_base":"1/90000"},{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":%q}}`, codec, rate, duration))
	}
	if d, e := probe(makeProbe("104.824583", "hevc", "30000/1001"), false); e != nil || d.DurationMS != 104825 {
		t.Fatal("legitimate complete historical clip rejected", e)
	}
	for _, b := range [][]byte{makeProbe("121", "h264", "30/1"), makeProbe("30", "h264", "61/1"), makeProbe("30", "hevc", "30/1")} {
		if _, e := probe(b, true); e == nil {
			t.Fatal("invalid canonical contract accepted")
		}
	}
	if frames, d, e := progress([]byte("frame=3126\nout_time_us=104824583\nprogress=end\n")); e != nil || frames != 3126 || d != 104824 {
		t.Fatal("whole timeline proof lost")
	}
	if _, _, e := progress([]byte("frame=3126\nout_time_us=104824583\nprogress=continue\n")); e == nil {
		t.Fatal("unfinished decoder accepted")
	}
	d := Derivative{Content: []byte{1, 2}, Poster: []byte{3}}
	raw := d.Content
	d.Erase()
	if raw[0] != 0 || d.Content != nil || d.Poster != nil {
		t.Fatal("owned RAM not retired")
	}
}
