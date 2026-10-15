import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:birdtie_client/src/workspace/public_moment_media_controller.dart';
import 'package:birdtie_client/src/workspace/public_moment_media_editor.dart';
import 'public_moment_media_controller_test.dart'
    show mediaOwner, mediaMoment, mediaPNG, mediaEmptySet, MediaMemoryJournal;

void main() {
  testWidgets(
    'embedded collection keeps real nine previews unchecked and provides no second text composer',
    (t) async {
      final client = MockClient(
        (r) async => http.Response(
          jsonEncode({'data': mediaEmptySet()}),
          200,
          headers: {'content-type': 'application/json'},
        ),
      );
      final c = PublicMomentMediaController(
        momentID: mediaMoment,
        expectedMomentRevision: 2,
        ownerID: () => mediaOwner,
        authorizationHeader: () => 'Bearer LOCAL',
        sourceCurrent: () => true,
        client: client,
        pendingStore: MediaMemoryJournal(),
        apiBaseUrl: 'https://media.test',
      );
      // Stream events from the bounded HTTP reader are real async work. Keep
      // this setup outside the widget fake clock, then exercise the editor.
      await t.runAsync(c.load);
      c.addSelectedBytes(List.generate(9, (_) => Uint8List.fromList(mediaPNG)));
      await t.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SingleChildScrollView(
              child: PublicMomentMediaEditor(controller: c, embedded: true),
            ),
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.byType(Image), findsNWidgets(9));
      expect(find.byType(TextField), findsNothing);
      expect(find.byType(AppBar), findsNothing);
      expect(find.byType(CheckboxListTile), findsNWidgets(9));
      expect(
        t
            .widgetList<CheckboxListTile>(find.byType(CheckboxListTile))
            .every((w) => w.value == false),
        isTrue,
      );
      expect(c.canPreview, isFalse);
      expect(find.text('动态图片 · 9/9'), findsOneWidget);
      await t.pumpWidget(const SizedBox.shrink());
      c.dispose();
      client.close();
    },
  );
}
