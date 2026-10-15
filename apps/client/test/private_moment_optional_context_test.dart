import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/moment_thread_api.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'owned EDIT content keeps original relationships without public geography',
    () {
      const id = '16800000-0000-4000-8000-000000000001';
      const contextId = '16800000-0000-4000-8000-000000000002';
      final wire = <String, dynamic>{
        'id': id,
        'cityId': '',
        'placeId': '',
        'title': '',
        'body': '内容独立于城市',
        'timePrecision': 'unknown',
        'locationPrecision': 'none',
        'status': 'published',
        'visibility': 'public',
        'revision': 3,
        'activityIds': [contextId],
        'communityId': contextId,
        'organizationId': contextId,
      };
      final moment = PrivateMoment.fromJson(wire);
      final entry = MomentThreadDraftEntry.owned(moment, locations: const []);
      expect(entry.momentID, id);
      expect(entry.input['cityId'], '');
      expect(entry.input['locations'], isEmpty);
      expect(entry.input['activityId'], contextId);
      expect(entry.input['communityId'], contextId);
      expect(entry.input['organizationId'], contextId);
      expect(entry.editInputHash, hasLength(64));
      expect(
        () => PrivateMoment.fromJson({...wire, 'locationPrecision': 'city'}),
        throwsFormatException,
      );
      expect(
        () => PrivateMoment.fromJson({...wire, 'locationPrecision': 'place'}),
        throwsFormatException,
      );
    },
  );
}
