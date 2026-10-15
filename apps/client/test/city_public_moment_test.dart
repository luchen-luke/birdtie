import 'package:birdtie_client/src/content/moment_control.dart';
import 'package:birdtie_client/src/workspace/public_moment_api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';
import 'moment_control_test.dart' show mcInput, mcOp, mcTarget;
import 'public_moment_api_test.dart' show publicMomentV3Data, momentReply;
import 'entity_share_pending_store_test.dart' show shareTarget;

Map<String, dynamic> cityMoment() => {
  ...publicMomentV3Data(photo: false),
  'schemaVersion': 'public-moment-v4',
  'cityName': '阿伯丁',
  'locationPrecision': 'city',
  'placeId': '',
  'placeName': '',
};

void main() {
  test('城市正文使用明确城市契约，不伪造地点或接受城市照片', () async {
    final api = PublicMomentApi(
      authorizationHeader: () => null,
      apiBaseUrl: 'https://api.example',
      client: MockClient((request) async {
        expect(request.headers['X-Birdtie-Public-Moment-Context'], 'city-v1');
        return momentReply(cityMoment());
      }),
    );
    addTearDown(api.dispose);
    final moment = await api.read(shareTarget);
    expect(moment.cityName, '阿伯丁');
    expect(moment.placeID, isEmpty);
    expect(moment.locationPrecision, 'city');
    expect(moment.photo, isNull);
    for (final fields in <Map<String, dynamic>>[
      {'placeName': '猜测地点'},
      {'locationPrecision': 'none'},
      {'cityName': ''},
      {
        'photo': {'privateImageId': mcTarget},
      },
      {'privateBody': '私人材料'},
    ]) {
      expect(
        () => PublicMoment.fromJson({...cityMoment(), ...fields}, shareTarget),
        throwsFormatException,
      );
    }
    expect(
      () => PublicMoment.fromJson({
        ...cityMoment(),
        'schemaVersion': 'public-moment-v3',
      }, shareTarget),
      throwsFormatException,
    );
  });

  test('城市发布和修改仍绑定原操作，拒绝无精度和隐藏上下文', () {
    for (final action in ['PUBLISH', 'UPDATE_PUBLIC']) {
      final input = MomentControlInput(
        operationID: mcOp,
        action: action,
        momentID: mcTarget,
        expectedRevision: 2,
        input: mcInput(),
      );
      expect(input.valid, isTrue);
      for (final fields in <Map<String, Object?>>[
        {'locationPrecision': 'none'},
        {'locationPrecision': 'place'},
        {'placeId': mcTarget},
        {'activityId': mcTarget},
      ]) {
        expect(
          () => MomentControlInput(
            operationID: mcOp,
            action: action,
            momentID: mcTarget,
            expectedRevision: 2,
            input: {...mcInput(), ...fields},
          ),
          throwsFormatException,
        );
      }
    }
  });
}
