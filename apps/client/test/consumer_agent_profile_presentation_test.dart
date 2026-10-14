import 'agent_memory_review_page_fixture.dart';
import 'package:birdtie_client/src/app/birdtie_surfaces.dart';
import 'package:birdtie_client/src/workspace/agent_profile_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/testing.dart';

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'agent_profile_api_test.dart';
import 'agent_memory_correction_api_test.dart' show correctionMemoryRaw;

void main() {
  for (final dark in [false, true]) {
    for (final scale in [1.0, 1.3, 2.0]) {
      testWidgets(
        'private memory list displays the actual statement and reaches its controls $dark/$scale',
        (t) async {
          t.view.devicePixelRatio = 1;
          t.view.physicalSize = const Size(320, 800);
          addTearDown(t.view.resetDevicePixelRatio);
          addTearDown(t.view.resetPhysicalSize);
          final auth = SeedTestAuth()..owner = profileOwner;
          addTearDown(auth.dispose);
          final client = MockClient(
            (r) async => profileResponse(profileData(r.url.path)),
          );
          addTearDown(client.close);
          await t.pumpWidget(
            MaterialApp(
              theme: birdtieTheme(dark ? Brightness.dark : Brightness.light),
              builder: (context, child) => MediaQuery(
                data: MediaQuery.of(
                  context,
                ).copyWith(textScaler: TextScaler.linear(scale)),
                child: child!,
              ),
              home: AgentProfilePage(
                auth: auth,
                client: client,
                apiBaseUrl: 'https://profile-consumer.test',
              ),
            ),
          );
          await t.pumpAndSettle();
          final memory = find.byKey(
            const ValueKey('profile-memory-$profileMemory'),
          );
          await t.scrollUntilVisible(
            memory,
            150,
            maxScrolls: 100,
            scrollable: find.byType(Scrollable).first,
          );
          expect(
            find.descendant(of: memory, matching: find.text('我偏好徒步活动')),
            findsOneWidget,
          );
          expect(
            t
                .widget<Text>(
                  find.descendant(of: memory, matching: find.text('我偏好徒步活动')),
                )
                .maxLines,
            isNull,
          );
          expect(t.getSize(memory).height, greaterThanOrEqualTo(48));
          await t.tap(memory);
          await t.pumpAndSettle();
          final manage = find.widgetWithText(OutlinedButton, '管理与纠正记忆');
          await t.scrollUntilVisible(
            manage,
            160,
            maxScrolls: 100,
            scrollable: find.byType(Scrollable).first,
          );
          expect(t.getSize(manage).height, greaterThanOrEqualTo(48));
          expect(find.text('仅自己可见'), findsOneWidget);
          expect(t.takeException(), isNull);
          await t.pumpWidget(const SizedBox.shrink());
        },
      );
    }
  }
  testWidgets('expired memory rows do not expose the retained summary', (
    t,
  ) async {
    final auth = SeedTestAuth()..owner = profileOwner;
    addTearDown(auth.dispose);
    final client = MockClient(
      (r) async => profileResponse(
        r.url.path.endsWith('/agent-memories/page')
            ? memoryReviewPageFixture(
                owner: profileOwner,
                agent: profileAgent,
                collection: 'MEMORIES',
                items: [
                  correctionMemoryRaw(at: DateTime.now().toUtc())
                    ..['status'] = 'EXPIRED',
                ],
              )
            : profileData(r.url.path),
      ),
    );
    addTearDown(client.close);
    await t.pumpWidget(
      MaterialApp(
        home: AgentProfilePage(
          auth: auth,
          client: client,
          apiBaseUrl: 'https://profile-consumer.test',
        ),
      ),
    );
    await t.pumpAndSettle();
    await t.scrollUntilVisible(
      find.text('已过期的记忆'),
      140,
      maxScrolls: 100,
      scrollable: find.byType(Scrollable).first,
    );
    expect(find.text('我偏好徒步活动'), findsNothing);
    await t.pumpWidget(const SizedBox.shrink());
  });
}
