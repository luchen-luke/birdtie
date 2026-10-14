import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/inbox_review_result_page.dart';
import 'package:birdtie_client/src/workspace/remote_inbox_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const itemID = '74100000-0000-4000-8000-000000000010';
const candidateID = '74100000-0000-4000-8000-000000000011';
InboxItem item() => InboxItem(
  id: itemID,
  category: 'updates',
  title: '审核通知',
  detail: '',
  resourceType: 'place_candidate',
  resourceID: candidateID,
  createdAt: DateTime.utc(2026, 10, 15),
);
Map<String, Object?> result({String status = 'published', String? target}) => {
  'schemaVersion': 'inbox-review-result-v1',
  'itemId': itemID,
  'kind': 'place_candidate',
  'status': status,
  'targetAvailable': target != null,
  if (target != null) 'targetId': target,
};
void main() {
  test(
    'review result requires exact notification and excludes candidate material',
    () {
      expect(InboxReviewResult.decode(result(), item()).targetID, isNull);
      expect(
        () => InboxReviewResult.decode({
          ...result(),
          'itemId': candidateID,
        }, item()),
        throwsFormatException,
      );
      expect(
        () => InboxReviewResult.decode({
          ...result(),
          'reviewNote': 'private',
        }, item()),
        throwsFormatException,
      );
      expect(
        () => InboxReviewResult.decode(
          result(status: 'rejected', target: candidateID),
          item(),
        ),
        throwsFormatException,
      );
    },
  );
  testWidgets(
    'current result survives withdrawn formal place; late account response is hidden',
    (t) async {
      final changes = ValueNotifier(0);
      var live = true, delayed = false;
      var writes = 0;
      final pending = Completer<http.Response>();
      final source = RemoteInboxSource(
        authorizationHeader: () => 'Bearer fixture',
        apiBaseUrl: 'https://fixture',
        identityChanges: changes,
        isCurrent: () => live,
        client: MockClient((r) async {
          if (r.method != 'GET') writes++;
          expect(r.url.path, '/v1/me/inbox/$itemID/review-result');
          if (delayed) return pending.future;
          return http.Response(jsonEncode({'data': result()}), 200);
        }),
      );
      await t.pumpWidget(
        MaterialApp(
          home: InboxReviewResultPage(
            item: item(),
            source: source,
            current: () => live,
            identityChanges: changes,
            ownerID: () => candidateID,
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('审核通过，已发布'), findsOneWidget);
      expect(find.text('正式内容当前不可访问，审核结果仍保留。'), findsOneWidget);
      expect(find.text('查看正式地点'), findsNothing);
      expect(writes, 0);
      delayed = true;
      await t.tap(find.text('重新读取审核结果'));
      await t.pump();
      live = false;
      changes.value++;
      await t.pump();
      pending.complete(
        http.Response(jsonEncode({'data': result(target: candidateID)}), 200),
      );
      await t.pumpAndSettle();
      expect(find.text('查看正式地点'), findsNothing);
      expect(find.text('审核通过，已发布'), findsNothing);
      await t.pumpWidget(const SizedBox());
      source.dispose();
      changes.dispose();
    },
  );
}
