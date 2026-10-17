// Package cityartwork owns the narrow display projection of reviewed system
// destination artwork. It never authorizes original archives or personal media.
package cityartwork

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
)

const Edge = 324
const MaxBytes = 1024 * 1024

var ErrNotFound = errors.New("city artwork not found")
var ErrUnavailable = errors.New("city artwork unavailable")
var cityPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,119}$`)
var versionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidCity(id string) bool   { return cityPattern.MatchString(id) }
func ValidVersion(v string) bool { return versionPattern.MatchString(v) }
func DescriptorPath(id, version string) string {
	return "/v1/cities/" + id + "/artwork/" + version + "/descriptor"
}
func ContentPath(id, version string) string {
	return "/v1/cities/" + id + "/artwork/" + version + "/content"
}

type Metadata struct {
	Version        string `json:"version"`
	DescriptorPath string `json:"descriptorPath"`
}

type Descriptor struct {
	CityID      string `json:"cityId"`
	Version     string `json:"version"`
	ContentPath string `json:"contentPath"`
	ContentType string `json:"contentType"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ByteCount   int    `json:"byteCount"`
	SHA256      string `json:"sha256"`
}

func (d Descriptor) Valid() bool {
	return ValidCity(d.CityID) && ValidVersion(d.Version) &&
		d.ContentPath == ContentPath(d.CityID, d.Version) && d.ContentType == "image/png" &&
		d.Width > 0 && d.Width <= Edge && d.Height > 0 && d.Height <= Edge &&
		d.ByteCount > 0 && d.ByteCount <= MaxBytes && ValidVersion(d.SHA256)
}

type Content struct {
	Descriptor Descriptor
	Bytes      []byte
}

func (c Content) Valid() bool {
	if !c.Descriptor.Valid() || len(c.Bytes) != c.Descriptor.ByteCount {
		return false
	}
	sum := sha256.Sum256(c.Bytes)
	return hex.EncodeToString(sum[:]) == c.Descriptor.SHA256
}

type Store interface {
	ReadCityArtwork(context.Context, string, string) (Content, error)
	ValidateCityArtwork(context.Context, string, string) error
}
