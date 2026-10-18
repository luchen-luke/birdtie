import 'dart:io';
import 'dart:typed_data';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:birdtie_client/src/city/public_city_map.dart';
import 'package:birdtie_client/src/city/native_city_map_io.dart';
import 'package:birdtie_client/src/workspace/organization_coordinate_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'batch_a_visual_test.dart' show captureKey, capture, visualTheme;

PublicCity coordinateCity({
  String provider = 'mapbox',
  String id = 'aberdeen-gb',
}) => PublicCity(
  id: id,
  name: '合成阿伯丁',
  region: 'GB',
  contentStatus: 'published',
  source: const PublicSource(
    label: '隔离fixture',
    maintainer: 'QA',
    freshness: 'current',
    updatedAt: null,
  ),
  map: PublicCityMap(
    provider: provider,
    latitude: 57.15,
    longitude: -2.1,
    defaultZoom: 13,
    sourceRef: 'isolated-coordinate-map',
  ),
);

void main() {
  final capturing =
      Platform.environment['BIRDTIE_B3_COORDINATE_CAPTURE'] == '1';
  setUpAll(() async {
    if (!capturing) return;
    for (final f in [
      ('BatchACJK', r'C:\Windows\Fonts\msyh.ttc'),
      (
        'MaterialIcons',
        r'D:\DevTools\flutter\bin\cache\artifacts\material_fonts\MaterialIcons-Regular.otf',
      ),
    ]) {
      await (FontLoader(f.$1)..addFont(
            Future.value(ByteData.sublistView(await File(f.$2).readAsBytes())),
          ))
          .load();
    }
  });
  test(
    'coordinate editor callback is optional and leaves ordinary entity map API intact',
    () {
      final public = PublicCityMapView(
        city: coordinateCity(),
        places: const [],
        onPlaceSelected: (_) {},
      );
      final native = NativeCityMapView(
        city: coordinateCity(),
        places: const [],
        accessToken: 'isolated-pk',
        onPlaceSelected: (_) {},
      );
      expect(public.onCoordinateSelected, isNull);
      expect(native.onCoordinateSelected, isNull);
      expect(public.onPlaceSelected, isNotNull);
      expect(native.onPlaceSelected, isNotNull);
      expect(
        organizationCoordinateUnavailable(coordinateCity(provider: 'amap')),
        isNull,
      );
      expect(
        organizationCoordinateUnavailable(
          coordinateCity(provider: 'unavailable'),
        ),
        contains('尚无'),
      );
    },
  );
  for (final mode in [
    'select',
    'cancel',
    'actorABA',
    'unavailable',
    'oldVersion',
  ]) {
    testWidgets(
      'coordinate picker normal390 $mode returns only explicitly current draft input',
      (t) async {
        t.view.physicalSize = const Size(390, 844);
        t.view.devicePixelRatio = 1;
        addTearDown(t.view.resetPhysicalSize);
        addTearDown(t.view.resetDevicePixelRatio);
        final identity = ValueNotifier(0), nav = GlobalKey<NavigatorState>();
        var current = true;
        OrganizationDraftCoordinate? result;
        late void Function(double, double) select;
        late ValueChanged<String> unavailable;
        Widget factory(
          BuildContext _,
          PublicCity city,
          void Function(double, double) point,
          ValueChanged<String> failed,
        ) {
          select = point;
          unavailable = failed;
          return const ColoredBox(
            color: Color(0xffeff4f0),
            child: Center(child: Text('隔离手势样例 · 非真实地图')),
          );
        }

        await t.pumpWidget(
          RepaintBoundary(
            key: captureKey,
            child: MaterialApp(
              navigatorKey: nav,
              theme: capturing ? visualTheme() : null,
              debugShowCheckedModeBanner: false,
              home: Scaffold(
                body: Builder(
                  builder: (context) => TextButton(
                    onPressed: () async {
                      result =
                          await Navigator.push<OrganizationDraftCoordinate>(
                            context,
                            MaterialPageRoute(
                              builder: (_) => OrganizationCoordinatePickerPage(
                                city: coordinateCity(),
                                identityChanges: identity,
                                sourceCurrent: () => current,
                                mapBuilder: factory,
                              ),
                            ),
                          );
                    },
                    child: const Text('打开组织坐标选择'),
                  ),
                ),
              ),
            ),
          ),
        );
        await t.tap(find.text('打开组织坐标选择'));
        await t.pumpAndSettle();
        final use = find.widgetWithText(FilledButton, '使用此坐标填写草稿');
        expect(t.widget<FilledButton>(use).onPressed, isNull);
        select(double.nan, -2.1);
        select(57, 181);
        await t.pumpAndSettle();
        expect(t.widget<FilledButton>(use).onPressed, isNull);
        select(57.15231, -2.09877);
        await t.pumpAndSettle();
        expect(find.text('WGS84：57.152310，-2.098770'), findsOneWidget);
        final oldConfirm = t.widget<FilledButton>(use).onPressed!;
        if (mode == 'select') {
          if (capturing) await capture(t, 'coordinate-after-selected');
          await t.tap(find.text('使用此坐标填写草稿'));
          await t.pumpAndSettle();
          expect(result, (latitude: 57.15231, longitude: -2.09877));
        } else if (mode == 'cancel') {
          await t.tap(find.text('取消选点'));
          await t.pumpAndSettle();
          expect(result, isNull);
        } else if (mode == 'actorABA') {
          current = false;
          identity.value++;
          current = true;
          identity.value++;
          oldConfirm();
          select(58, -3);
          await t.pumpAndSettle();
          expect(find.text('身份或城市已变化，请返回重新选择。'), findsOneWidget);
          expect(find.textContaining('57.152310'), findsNothing);
          expect(result, isNull);
        } else if (mode == 'unavailable') {
          unavailable('synthetic tile failure');
          await t.pumpAndSettle();
          oldConfirm();
          select(58, -3);
          await t.pumpAndSettle();
          expect(t.widget<FilledButton>(use).onPressed, isNull);
          expect(find.textContaining('57.152310'), findsNothing);
          expect(result, isNull);
          if (capturing) await capture(t, 'coordinate-after-map-unavailable');
        } else {
          select(58.01, -3.01);
          await t.pumpAndSettle();
          oldConfirm();
          await t.pumpAndSettle();
          expect(result, isNull);
          expect(find.text('WGS84：58.010000，-3.010000'), findsOneWidget);
          await t.tap(find.text('使用此坐标填写草稿'));
          await t.pumpAndSettle();
          expect(result, (latitude: 58.01, longitude: -3.01));
        }
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        await t.pumpAndSettle();
        identity.dispose();
      },
    );
  }
  testWidgets(
    'coordinate picker AMap uses canonical adapter and missing SDK configuration cannot invent a point',
    (t) async {
      final identity = ValueNotifier(0);
      var returned = false;
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                onPressed: () async {
                  final point =
                      await Navigator.push<OrganizationDraftCoordinate>(
                        context,
                        MaterialPageRoute(
                          builder: (_) => OrganizationCoordinatePickerPage(
                            city: coordinateCity(provider: 'amap'),
                            identityChanges: identity,
                            sourceCurrent: () => true,
                          ),
                        ),
                      );
                  expect(point, isNull);
                  returned = true;
                },
                child: const Text('从位置页进入选点'),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('从位置页进入选点'));
      await t.pumpAndSettle();
      expect(find.textContaining('可核对的 WGS84'), findsNothing);
      expect(
        t
            .widget<FilledButton>(
              find.widgetWithText(FilledButton, '使用此坐标填写草稿'),
            )
            .onPressed,
        isNull,
      );
      expect(find.byType(PublicCityMapView), findsOneWidget);
      expect(find.text('取消选点'), findsOneWidget);
      await t.tap(find.text('取消选点'));
      await t.pumpAndSettle();
      expect(returned, isTrue);
      expect(find.byType(OrganizationCoordinatePickerPage), findsNothing);
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      identity.dispose();
    },
  );
}
