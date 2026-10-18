import 'dart:async';
import 'package:birdtie_client/src/city/amap_destination_search.dart';
import 'package:birdtie_client/src/city/amap_session_consent.dart';
import 'package:birdtie_client/src/city/destination_search_service.dart';
import 'package:birdtie_client/src/city/map_privacy_consent.dart';
import 'package:birdtie_client/src/city/map_coordinate_boundary.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late MapPrivacyConsentController consent, previous;
  setUp(() {
    debugDefaultTargetPlatformOverride = TargetPlatform.android;
    consent = MapPrivacyConsentController(
      store: MemoryMapPrivacyConsentStore(),
    );
    previous = AMapSessionConsent.replaceControllerForTesting(consent);
  });
  tearDown(() {
    debugDefaultTargetPlatformOverride = null;
    AMapSessionConsent.replaceControllerForTesting(previous);
    consent.dispose();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(AMapDestinationSearchService.channel, null);
  });
  test('no SDK call before explicit session consent', () async {
    var calls = 0;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(AMapDestinationSearchService.channel, (
          _,
        ) async {
          calls++;
          return null;
        });
    final service = AMapDestinationSearchService();
    expect((await service.search('上海')).domesticUnavailable, isTrue);
    expect(calls, 0);
    service.dispose();
  });
  test(
    'native GCJ result converts once, remains temporary and has no internal city',
    () async {
      await consent.setDecision(true);
      final gcj = MapCoordinateBoundary.toAmap(31.2304, 121.4737);
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(AMapDestinationSearchService.channel, (
            call,
          ) async {
            expect(call.method, 'search');
            expect(call.arguments['consent'], true);
            expect((call.arguments as Map).containsKey('key'), isFalse);
            return {
              'attribution': '高德地图',
              'items': [
                {
                  'id': '310000:place:上海市',
                  'name': '上海市',
                  'region': '上海市 · 中国',
                  'countryCode': 'CN',
                  'featureType': 'place',
                  'coordinateSystem': 'gcj02',
                  'latitude': gcj.latitude,
                  'longitude': gcj.longitude,
                },
                {
                  'id': '330000:region:浙江省',
                  'name': '浙江省',
                  'region': '浙江省 · 中国',
                  'countryCode': 'CN',
                  'featureType': 'region',
                  'coordinateSystem': 'gcj02',
                  'latitude': 29.2,
                  'longitude': 120.2,
                },
              ],
            };
          });
      final service = AMapDestinationSearchService();
      final items = (await service.search('上海')).destinations;
      final item = items.first;
      expect(item.latitude, closeTo(31.2304, .000001));
      expect(item.longitude, closeTo(121.4737, .000001));
      expect(item.provider, 'amap');
      expect(item.isTemporary, isTrue);
      expect(item.internalCityID, isNull);
      expect(item.featureType, 'place');
      expect(item.defaultZoom, 11);
      expect(items.last.featureType, 'region');
      expect(items.last.defaultZoom, 7);
      service.dispose();
    },
  );
  test('cancellation retires a late native response', () async {
    await consent.setDecision(true);
    final pending = Completer<Object?>(), abort = Completer<void>();
    var cancels = 0;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(AMapDestinationSearchService.channel, (
          call,
        ) async {
          if (call.method == 'cancel') {
            cancels++;
            return null;
          }
          return pending.future;
        });
    final service = AMapDestinationSearchService();
    final result = service.search('上海', abortTrigger: abort.future);
    final rejected = expectLater(result, throwsA(isA<Exception>()));
    abort.complete();
    await Future<void>.delayed(Duration.zero);
    pending.complete({'attribution': '高德地图', 'items': []});
    await rejected;
    expect(cancels, greaterThanOrEqualTo(1));
    service.dispose();
  });

  test(
    'revocation and re-agreement cannot adopt an old native search reply',
    () async {
      await consent.setDecision(true);
      final pending = Completer<Object?>();
      var cancels = 0;
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(AMapDestinationSearchService.channel, (
            call,
          ) async {
            if (call.method == 'cancel') {
              cancels++;
              return null;
            }
            return pending.future;
          });
      final service = AMapDestinationSearchService();
      final job = service.search('上海');
      final rejected = expectLater(
        job,
        throwsA(isA<DestinationSearchFailure>()),
      );
      await consent.setDecision(false);
      await consent.setDecision(true);
      pending.complete({'attribution': '高德地图', 'items': []});
      await rejected;
      expect(cancels, greaterThan(0));
      service.dispose();
    },
  );
}
