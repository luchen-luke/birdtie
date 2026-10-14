import 'dart:async';
import 'package:birdtie_client/src/workspace/person_avatar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'person_avatar_fixture.dart';

void main() {
  testWidgets(
    'a permitted thumbnail is shown and evicted on workspace change',
    (t) async {
      var current = true;
      final changes = ValueNotifier(0);
      addTearDown(changes.dispose);
      final client = MockClient(
        (r) async => http.Response.bytes(avatarJPEG(), 200),
      );
      addTearDown(client.close);
      await t.pumpWidget(
        MaterialApp(
          home: PersonAvatar(
            metadata: PersonAvatarMetadata.fromJson(
              avatarMetadata(),
              avatarOwner,
            ),
            authorizationHeader: () => 'Bearer A',
            baseUrl: 'http://local.test',
            client: client,
            identityChanges: changes,
            current: () => current,
          ),
        ),
      );
      await t.runAsync(() async {
        await Future<void>.delayed(const Duration(milliseconds: 80));
      });
      await t.pumpAndSettle();
      expect(find.byType(Image), findsOneWidget);
      current = false;
      changes.value++;
      await t.pumpAndSettle();
      expect(find.byType(Image), findsNothing);
      expect(find.byIcon(Icons.person_outline), findsOneWidget);
    },
  );
  testWidgets('late image read cannot attach to retired identity', (t) async {
    var current = true;
    final changes = ValueNotifier(0);
    addTearDown(changes.dispose);
    final reply = Completer<http.Response>();
    final client = MockClient((r) => reply.future);
    addTearDown(client.close);
    await t.pumpWidget(
      MaterialApp(
        home: PersonAvatar(
          metadata: PersonAvatarMetadata.fromJson(
            avatarMetadata(),
            avatarOwner,
          ),
          authorizationHeader: () => 'Bearer A',
          baseUrl: 'http://local.test',
          client: client,
          identityChanges: changes,
          current: () => current,
        ),
      ),
    );
    await t.pump();
    current = false;
    changes.value++;
    await t.pump();
    reply.complete(http.Response.bytes(avatarJPEG(), 200));
    await t.pumpAndSettle();
    expect(find.byType(Image), findsNothing);
    expect(find.byIcon(Icons.person_outline), findsOneWidget);
  });
}
