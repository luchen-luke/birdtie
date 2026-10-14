import 'dart:convert';

import 'package:birdtie_client/src/city/native_city_map_io.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mapbox_maps_flutter/mapbox_maps_flutter.dart';

void main() {
  test('camera evidence JSON preserves exact numeric map state only', () {
    // Synthetic serialization unit input, not native map acceptance evidence.
    final camera = CameraState(
      center: Point(coordinates: Position(-2.0943456789, 57.1497654321)),
      padding: MbxEdgeInsets(top: 12, left: 16, bottom: 48, right: 16),
      zoom: 13.1256789,
      pitch: 28.5123456,
      bearing: 315.9876543,
    );
    final json =
        jsonDecode(jsonEncode(nowNorthCameraEvidenceSnapshot(camera)))
            as Map<String, dynamic>;
    expect(json.keys.toSet(), {'center', 'zoom', 'pitch', 'bearing'});
    expect(json['center'], {
      'longitude': -2.0943456789,
      'latitude': 57.1497654321,
    });
    expect(json['zoom'], 13.1256789);
    expect(json['pitch'], 28.5123456);
    expect(json['bearing'], 315.9876543);
    expect(camera.bearing, 315.9876543);
    expect(camera.padding.bottom, 48);
  });
}
