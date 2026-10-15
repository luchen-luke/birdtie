package media

import (
	"bytes"
	"encoding/binary"
)

// Read only the bounded IFD0 orientation needed for pixel transformation. GPS,
// capture time and other EXIF never become identity, visit or location evidence.
func tiffOrientation(data []byte) (int, error) {
	return tiffOrientationWithPolicy(data, false)
}

func tiffOrientationWithPolicy(data []byte, legacyPrivate bool) (int, error) {
	if len(data) < 8 {
		return 0, ErrImageInvalid
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, ErrImageInvalid
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0, ErrImageInvalid
	}
	offset := int64(order.Uint32(data[4:8]))
	if offset < 8 || offset > int64(len(data))-2 {
		return 0, ErrImageInvalid
	}
	count := int64(order.Uint16(data[offset : offset+2]))
	if count > 1024 || offset+2+count*12+4 > int64(len(data)) {
		return 0, ErrImageInvalid
	}
	orientation, seen := 1, false
	for i := int64(0); i < count; i++ {
		entry := data[offset+2+i*12 : offset+2+(i+1)*12]
		if order.Uint16(entry[:2]) != 0x112 {
			continue
		}
		typ := order.Uint16(entry[2:4])
		if seen || (typ != 3 && !(legacyPrivate && typ == 4)) || order.Uint32(entry[4:8]) != 1 {
			return 0, ErrImageInvalid
		}
		seen = true
		value := uint32(order.Uint16(entry[8:10]))
		if typ == 4 {
			value = order.Uint32(entry[8:12])
		}
		// Some authenticated historical encoders wrote an unspecified zero,
		// including with LONG type. Preserve their encoded frame; do not infer a
		// rotation from other EXIF. Ordinary uploads retain the strict contract.
		if legacyPrivate && value == 0 {
			orientation = 1
		} else {
			orientation = int(value)
		}
		if orientation < 1 || orientation > 8 {
			return 0, ErrImageInvalid
		}
	}
	return orientation, nil
}

func imageOrientation(data []byte, format string) (int, error) {
	return imageOrientationWithPolicy(data, format, false)
}

func imageOrientationWithPolicy(data []byte, format string, legacyPrivate bool) (int, error) {
	orientation, seen := 1, false
	if format == "jpeg" {
		if len(data) < 2 || !bytes.Equal(data[:2], []byte{0xff, 0xd8}) {
			return 0, ErrImageInvalid
		}
		for pos := 2; pos < len(data); {
			if data[pos] != 0xff {
				return 0, ErrImageInvalid
			}
			for pos < len(data) && data[pos] == 0xff {
				pos++
			}
			if pos >= len(data) {
				return 0, ErrImageInvalid
			}
			marker := data[pos]
			pos++
			if marker == 0xda || marker == 0xd9 {
				return orientation, nil
			}
			if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
				continue
			}
			if pos > len(data)-2 {
				return 0, ErrImageInvalid
			}
			length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			if length < 2 || length > len(data)-pos {
				return 0, ErrImageInvalid
			}
			payload := data[pos+2 : pos+length]
			if marker == 0xe1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
				if seen {
					return 0, ErrImageInvalid
				}
				seen = true
				var err error
				orientation, err = tiffOrientationWithPolicy(payload[6:], legacyPrivate)
				if err != nil {
					return 0, err
				}
			}
			pos += length
		}
		return 0, ErrImageInvalid
	}
	if format != "png" || len(data) < 8 {
		return 0, ErrImageInvalid
	}
	for pos := 8; pos <= len(data)-12; {
		length := int64(binary.BigEndian.Uint32(data[pos : pos+4]))
		if length > int64(len(data)-pos-12) {
			return 0, ErrImageInvalid
		}
		kind := string(data[pos+4 : pos+8])
		if kind == "acTL" || kind == "fcTL" || kind == "fdAT" {
			return 0, ErrImageType // Do not silently analyze one frame of APNG.
		}
		if kind == "eXIf" {
			if seen {
				return 0, ErrImageInvalid
			}
			seen = true
			var err error
			orientation, err = tiffOrientationWithPolicy(data[pos+8:int64(pos)+8+length], legacyPrivate)
			if err != nil {
				return 0, err
			}
		}
		pos += int(length) + 12
		if kind == "IEND" {
			return orientation, nil
		}
	}
	return 0, ErrImageInvalid
}
