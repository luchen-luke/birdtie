package threadrouteprovider

// Coordinate transform matches Birdtie's existing client boundary, adapted
// from wandergis/coordtransform (MIT), Copyright (c) 2015 记忆的残骸.
// License: apps/client/third_party/coordtransform.LICENSE. The numerical
// envelope is NOT used to decide country/provider. All HTTP output is WGS84.
import (
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"math"
)

func outside(p content.ThreadRoutePoint) bool {
	return p.Longitude < 72.004 || p.Longitude > 137.8347 || p.Latitude < .8293 || p.Latitude > 55.8271
}
func toAMap(p content.ThreadRoutePoint) content.ThreadRoutePoint {
	if outside(p) {
		return p
	}
	return transform(p)
}
func transform(p content.ThreadRoutePoint) content.ThreadRoutePoint {
	x, y := p.Longitude-105, p.Latitude-35
	wave := (20*math.Sin(6*x*math.Pi) + 20*math.Sin(2*x*math.Pi)) * 2 / 3
	lat := -100 + 2*x + 3*y + .2*y*y + .1*x*y + .2*math.Sqrt(math.Abs(x)) + wave + (20*math.Sin(y*math.Pi)+40*math.Sin(y*math.Pi/3))*2/3 + (160*math.Sin(y*math.Pi/12)+320*math.Sin(y*math.Pi/30))*2/3
	lon := 300 + x + 2*y + .1*x*x + .1*x*y + .1*math.Sqrt(math.Abs(x)) + wave + (20*math.Sin(x*math.Pi)+40*math.Sin(x*math.Pi/3))*2/3 + (150*math.Sin(x*math.Pi/12)+300*math.Sin(x*math.Pi/30))*2/3
	rad := p.Latitude * math.Pi / 180
	sin := math.Sin(rad)
	factor := 1 - .00669342162296594323*sin*sin
	root := math.Sqrt(factor)
	p.Latitude += lat * 180 / ((6378245 * (1 - .00669342162296594323)) / (factor * root) * math.Pi)
	p.Longitude += lon * 180 / (6378245 / root * math.Cos(rad) * math.Pi)
	return p
}
func fromAMap(p content.ThreadRoutePoint) (content.ThreadRoutePoint, bool) {
	if p.Longitude < 72.004-.05 || p.Longitude > 137.8347+.05 || p.Latitude < .8293-.05 || p.Latitude > 55.8271+.05 {
		return p, true
	}
	out := p
	for i := 0; i < 8; i++ {
		v := transform(out)
		lat, lon := v.Latitude-p.Latitude, v.Longitude-p.Longitude
		if math.Abs(lat) <= 1e-8 && math.Abs(lon) <= 1e-8 {
			if outside(out) {
				return p, true
			}
			return out, true
		}
		out.Latitude -= lat
		out.Longitude -= lon
	}
	return p, false
}
