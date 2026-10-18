import 'dart:async';

import 'package:birdtie_client/src/city/map_privacy_consent.dart';
import 'package:birdtie_client/src/city/map_privacy_sheet.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'one disclosure has genuine accept/refuse and respects a saved refusal',
    (t) async {
      t.view.physicalSize = const Size(390, 844);
      t.view.devicePixelRatio = 1;
      addTearDown(t.view.resetPhysicalSize);
      addTearDown(t.view.resetDevicePixelRatio);
      final store = MemoryMapPrivacyConsentStore();
      final c = MapPrivacyConsentController(store: store);
      var review = false;
      bool? result;
      await t.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: Center(
                child: FilledButton(
                  onPressed: () {
                    unawaited(
                      showMapServicePrivacySheet(
                        context,
                        controller: c,
                        current: () => true,
                        reviewExisting: review,
                      ).then((v) => result = v),
                    );
                  },
                  child: const Text('查看隐私说明'),
                ),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('查看隐私说明'));
      await t.pumpAndSettle();
      expect(find.text('地图服务隐私说明'), findsOneWidget);
      expect(find.text('同意'), findsOneWidget);
      expect(find.text('暂不使用'), findsOneWidget);
      expect(find.textContaining('启用'), findsNothing);
      expect(t.takeException(), isNull);
      await t.tap(find.text('暂不使用'));
      await t.pumpAndSettle();
      expect(result, false);
      expect(c.hasDecision, true);
      expect(c.accepted, false);
      await t.tap(find.text('查看隐私说明'));
      await t.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      review = true;
      await t.tap(find.text('查看隐私说明'));
      await t.pumpAndSettle();
      await t.tap(find.text('同意'));
      await t.pumpAndSettle();
      expect(result, true);
      expect(c.accepted, true);
      final restarted = MapPrivacyConsentController(store: store);
      await restarted.load();
      expect(restarted.accepted, true);
      await t.pumpWidget(const SizedBox.shrink());
      c.dispose();
      restarted.dispose();
    },
  );
  testWidgets(
    'retired context cannot save an agreement from an old disclosure',
    (t) async {
      final store = MemoryMapPrivacyConsentStore();
      final c = MapPrivacyConsentController(store: store);
      var current = true;
      bool? result;
      await t.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => Scaffold(
              body: FilledButton(
                onPressed: () {
                  unawaited(
                    showMapServicePrivacySheet(
                      context,
                      controller: c,
                      current: () => current,
                    ).then((v) => result = v),
                  );
                },
                child: const Text('查看'),
              ),
            ),
          ),
        ),
      );
      await t.tap(find.text('查看'));
      await t.pumpAndSettle();
      current = false;
      await t.tap(find.text('同意'));
      await t.pumpAndSettle();
      expect(result, false);
      expect(store.record, isNull);
      expect(c.accepted, false);
      await t.pumpWidget(const SizedBox.shrink());
      c.dispose();
    },
  );
}
