import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_candidate_pending_store.dart';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_api.dart';
import 'package:birdtie_client/src/workspace/agent_memory_candidate_controller.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_api.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_controller.dart';
import 'package:birdtie_client/src/workspace/agent_memory_correction_pending_store.dart';
import 'package:birdtie_client/src/workspace/agent_memory_review_page_model.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'agent_memory_candidate_api_test.dart';
import 'agent_memory_correction_api_test.dart';
import 'agent_memory_correction_page_test.dart'
    show correctionPageHarness, correctionSettle;
import 'agent_memory_review_page_fixture.dart';
import 'agent_seed_sheet_test.dart' show SeedTestAuth;

String reviewID(int n) =>
    '73000000-0000-4000-8000-${n.toString().padLeft(12, '0')}';
Map<String, dynamic> reviewCandidate(int n) =>
    candidateWire()..['id'] = reviewID(n);
Map<String, dynamic> reviewMemory(int n) =>
    correctionMemoryRaw(at: DateTime.now().toUtc())
      ..['id'] = reviewID(n)
      ..['summary'] = '本人记忆 $n';
http.Response candidatePaged(List<dynamic> rows, {String? cursor}) =>
    candidateResponse(
      memoryReviewPageFixture(
        owner: candidateOwner,
        agent: candidateAgent,
        collection: 'CANDIDATES',
        items: rows,
        nextCursor: cursor,
      ),
    );
http.Response correctionPaged(
  String collection,
  List<dynamic> rows, {
  String? cursor,
}) => correctionResponse({
  'data': memoryReviewPageFixture(
    owner: correctionOwner,
    agent: correctionAgent,
    collection: collection,
    items: rows,
    nextCursor: cursor,
  ),
});

AgentMemoryCandidateController candidatePagingController(
  http.Client client, {
  String? Function()? owner,
}) => AgentMemoryCandidateController(
  api: AgentMemoryCandidateAPI(client: client, apiBaseUrl: 'http://fixture'),
  authorizationHeader: () => 'Bearer synthetic',
  accountID: owner ?? () => candidateOwner,
  organizationWorkspaceID: () => null,
  pendingStore: MemoryAgentCandidatePendingStore(),
);

void main() {
  test(
    'closed private page rejects forged permissions, identities and unbounded receipts',
    () {
      final clock = DateTime.now().toUtc();
      Map<String, dynamic> wire() => memoryReviewPageFixture(
        owner: candidateOwner,
        agent: candidateAgent,
        collection: 'CANDIDATES',
        at: clock,
        items: [for (var n = 1; n <= 20; n++) reviewCandidate(n)],
        nextCursor: 'cursor_1',
      );
      AgentMemoryReviewPage<HumanMemoryCandidate> read(dynamic value) =>
          AgentMemoryReviewPage.read(
            value,
            owner: candidateOwner,
            collection: 'CANDIDATES',
            now: clock,
            parse: (v) => HumanMemoryCandidate.read(v, candidateOwner),
            id: (v) => v.id,
            agent: (v) => v.raw['agentId'],
          );
      expect(read(wire()).items, hasLength(20));
      for (final change in [
        'unknown',
        'grant',
        'owner',
        'agent',
        'collection',
        'size',
        'duplicate',
        'cursor',
        'shortCursorPage',
        'expired',
        'lease',
      ]) {
        final w = wire();
        switch (change) {
          case 'unknown':
            w['approved'] = true;
          case 'grant':
            w['modelAccess'] = true;
          case 'owner':
            w['owner']['id'] = candidateAgent;
          case 'agent':
            w['agentId'] = candidateOwner;
          case 'collection':
            w['collection'] = 'MEMORIES';
          case 'size':
            (w['items'] as List).add(reviewCandidate(21));
          case 'duplicate':
            (w['items'] as List)[1] = reviewCandidate(1);
          case 'cursor':
            w['nextCursor'] = 'a' * 1025;
          case 'shortCursorPage':
            (w['items'] as List).removeLast();
          case 'expired':
            w['expiresAt'] = clock.toIso8601String();
          case 'lease':
            w['expiresAt'] = clock
                .add(const Duration(minutes: 3))
                .toIso8601String();
        }
        expect(() => read(w), throwsFormatException, reason: change);
      }
    },
  );

  test(
    'candidate page uses actual bodyless GET, opaque cursor and original authorization',
    () async {
      final seen = <http.Request>[];
      final client = MockClient((r) async {
        seen.add(r);
        return candidatePaged([reviewCandidate(1)]);
      });
      final api = AgentMemoryCandidateAPI(
        client: client,
        apiBaseUrl: 'http://fixture',
      );
      addTearDown(api.dispose);
      addTearDown(client.close);
      await api.page('Bearer synthetic', candidateOwner, cursor: 'abc_123-Z');
      expect(seen.single.method, 'GET');
      expect(seen.single.url.path, '/v1/me/agent-memory-candidates/page');
      expect(seen.single.url.queryParameters, {'cursor': 'abc_123-Z'});
      expect(seen.single.headers['Authorization'], 'Bearer synthetic');
      expect(seen.single.body, isEmpty);
      for (final cursor in ['', 'bad+value', 'a' * 1025]) {
        await expectLater(
          api.page('Bearer synthetic', candidateOwner, cursor: cursor),
          throwsFormatException,
        );
      }
      expect(seen, hasLength(1));
    },
  );

  test(
    'candidate pagination reaches 61 rows with one request per page',
    () async {
      final seen = <http.Request>[];
      final client = MockClient((r) async {
        seen.add(r);
        final start = switch (r.url.queryParameters['cursor']) {
          null => 1,
          'page_2' => 21,
          'page_3' => 41,
          'page_4' => 61,
          _ => throw StateError('wrong cursor'),
        };
        return candidatePaged(
          [
            for (var n = start; n <= (start == 61 ? 61 : start + 19); n++)
              reviewCandidate(n),
          ],
          cursor: start == 61
              ? null
              : 'page_${start == 1
                    ? 2
                    : start == 21
                    ? 3
                    : 4}',
        );
      });
      final c = candidatePagingController(client);
      addTearDown(c.dispose);
      addTearDown(client.close);
      await c.load();
      expect(c.records, hasLength(20));
      await c.loadMore();
      await c.loadMore();
      await c.loadMore();
      expect(c.records, hasLength(61));
      expect(c.records.last.id, reviewID(61));
      expect(c.hasMore, isFalse);
      expect(seen, hasLength(4));
      expect(seen.map((r) => r.url.query), [
        '',
        'cursor=page_2',
        'cursor=page_3',
        'cursor=page_4',
      ]);
      expect(seen.every((r) => r.method == 'GET' && r.body.isEmpty), isTrue);
      await c.loadMore();
      expect(seen, hasLength(4));
    },
  );

  test(
    'overlapping candidate pages append unique records and never extend authority',
    () async {
      var reads = 0;
      final client = MockClient((r) async {
        reads++;
        return r.url.query.isEmpty
            ? candidatePaged([
                for (var n = 1; n <= 20; n++) reviewCandidate(n),
              ], cursor: 'next')
            : candidatePaged([reviewCandidate(20), reviewCandidate(21)]);
      });
      final c = candidatePagingController(client);
      addTearDown(c.dispose);
      addTearDown(client.close);
      await c.load();
      await c.loadMore();
      expect(c.records, hasLength(21));
      expect(c.records.map((r) => r.id).toSet(), hasLength(21));
      expect(c.preview, isNull);
      expect(c.hasPendingAcceptance, isFalse);
      expect(reads, 2);
    },
  );

  for (final status in [409, 403]) {
    test(
      'candidate next-page $status clears old cursor; revoked access clears private rows',
      () async {
        var reads = 0;
        final client = MockClient((r) async {
          reads++;
          return r.url.query.isEmpty
              ? candidatePaged([
                  for (var n = 1; n <= 20; n++) reviewCandidate(n),
                ], cursor: 'next')
              : http.Response('{}', status);
        });
        final c = candidatePagingController(client);
        addTearDown(c.dispose);
        addTearDown(client.close);
        await c.load();
        await c.loadMore();
        expect(c.hasMore, isFalse);
        expect(c.records, hasLength(status == 403 ? 0 : 20));
        expect(c.personal, status != 403);
        expect(c.error, isNotNull);
        await c.loadMore();
        expect(reads, 2);
        if (status == 409) {
          await c.load();
          expect(c.records, hasLength(20));
          expect(reads, 3);
        }
      },
    );
  }

  test(
    'late next page after owner ABA cannot restore rows or old cursor',
    () async {
      String? owner = candidateOwner;
      final pending = Completer<http.Response>();
      final entered = Completer<void>();
      final client = MockClient((r) async {
        if (r.url.query.isNotEmpty) {
          entered.complete();
          return pending.future;
        }
        return candidatePaged([
          for (var n = 1; n <= 20; n++) reviewCandidate(n),
        ], cursor: 'next');
      });
      final c = candidatePagingController(client, owner: () => owner);
      addTearDown(c.dispose);
      addTearDown(client.close);
      await c.load();
      final more = c.loadMore();
      await entered.future;
      owner = candidateAgent;
      c.synchronizeIdentity();
      owner = candidateOwner;
      c.synchronizeIdentity();
      pending.complete(candidatePaged([reviewCandidate(21)]));
      await more;
      expect(c.records, isEmpty);
      expect(c.hasMore, isFalse);
      expect(c.preview, isNull);
    },
  );

  test(
    'synchronous loading notice retires a page before the actual send boundary',
    () async {
      String? owner = candidateOwner;
      var reads = 0, armed = false;
      final client = MockClient((r) async {
        reads++;
        return candidatePaged([
          for (var n = 1; n <= 20; n++) reviewCandidate(n),
        ], cursor: 'next');
      });
      final c = candidatePagingController(client, owner: () => owner);
      addTearDown(c.dispose);
      addTearDown(client.close);
      await c.load();
      c.addListener(() {
        if (!armed || !c.busy) return;
        armed = false;
        owner = candidateAgent;
        c.synchronizeIdentity();
        owner = candidateOwner;
        c.synchronizeIdentity();
      });
      armed = true;
      await c.loadMore();
      expect(reads, 1);
      expect(c.records, isEmpty);
      expect(c.hasMore, isFalse);
    },
  );

  test(
    'specific preview and unknown submit block next-page reads and automatic replay',
    () async {
      var reads = 0, accepts = 0;
      final client = MockClient((r) async {
        if (r.method == 'GET') {
          reads++;
          return candidatePaged([
            for (var n = 1; n <= 20; n++) reviewCandidate(n),
          ], cursor: 'next');
        }
        if (r.url.path.endsWith('/preview')) {
          final body = jsonDecode(r.body) as Map;
          return candidateResponse(
            previewWire(body['previewId'] as String, reviewCandidate(1)),
          );
        }
        accepts++;
        throw StateError('lost receipt');
      });
      final c = candidatePagingController(client);
      addTearDown(c.dispose);
      addTearDown(client.close);
      await c.load();
      await c.prepare(c.records.first);
      expect(c.preview, isNotNull);
      await c.loadMore();
      expect(reads, 1);
      await c.accept();
      expect(c.unknownAccept, isTrue);
      await c.loadMore();
      await c.accept();
      expect(reads, 1);
      expect(accepts, 1);
    },
  );

  test(
    'correction memory and candidate cursors remain separate and duplicate rows do not accumulate',
    () async {
      final seen = <http.Request>[];
      final client = MockClient((r) async {
        seen.add(r);
        final memory = r.url.path == '/v1/me/agent-memories/page';
        expect(
          r.url.path,
          memory
              ? '/v1/me/agent-memories/page'
              : '/v1/me/agent-memory-candidates/page',
        );
        if (r.url.query.isEmpty)
          return correctionPaged(
            memory ? 'MEMORIES' : 'CANDIDATES',
            memory ? [for (var n = 1; n <= 20; n++) reviewMemory(n)] : [],
            cursor: memory ? 'memory_next' : null,
          );
        expect(r.url.queryParameters, {'cursor': 'memory_next'});
        return correctionPaged('MEMORIES', [
          reviewMemory(20),
          reviewMemory(21),
        ]);
      });
      final c = AgentMemoryCorrectionController(
        api: AgentMemoryCorrectionAPI(
          client: client,
          apiBaseUrl: 'http://fixture',
        ),
        authorizationHeader: () => 'Bearer synthetic',
        accountID: () => correctionOwner,
        pendingStore: MemoryAgentMemoryCorrectionPendingStore(),
      );
      addTearDown(c.dispose);
      addTearDown(client.close);
      await c.load();
      expect(c.hasMoreMemories, isTrue);
      expect(c.hasMoreCandidates, isFalse);
      await c.loadMoreMemories();
      expect(c.memories, hasLength(21));
      expect(c.memories.last.id, reviewID(21));
      expect(c.hasMoreMemories, isFalse);
      expect(c.canReview, isTrue);
      expect(c.preview, isNull);
      expect(c.hasPending, isFalse);
      expect(seen, hasLength(3));
    },
  );

  testWidgets(
    'existing correction page reaches the next real API page without losing first-page rows',
    (t) async {
      final auth = SeedTestAuth()..owner = correctionOwner;
      addTearDown(auth.dispose);
      var more = 0;
      final client = MockClient((r) async {
        if (r.url.path.endsWith('agent-memory-candidates/page'))
          return correctionPaged('CANDIDATES', []);
        if (r.url.query.isNotEmpty) {
          more++;
          return correctionPaged('MEMORIES', [reviewMemory(21)]);
        }
        return correctionPaged('MEMORIES', [
          for (var n = 1; n <= 20; n++) reviewMemory(n),
        ], cursor: 'memory_next');
      });
      addTearDown(client.close);
      await t.pumpWidget(
        correctionPageHarness(
          auth,
          client,
          MemoryAgentMemoryCorrectionPendingStore(),
        ),
      );
      await correctionSettle(t);
      final button = find.widgetWithText(OutlinedButton, '继续读取记忆');
      await t.scrollUntilVisible(
        button,
        240,
        maxScrolls: 100,
        scrollable: find.byType(Scrollable).first,
      );
      await t.ensureVisible(button);
      await t.tap(button);
      await correctionSettle(t);
      expect(more, 1);
      await t.scrollUntilVisible(
        find.text('本人记忆 21'),
        160,
        maxScrolls: 100,
        scrollable: find.byType(Scrollable).first,
      );
      expect(find.text('本人记忆 21'), findsOneWidget);
      expect(button, findsNothing);
      await t.scrollUntilVisible(
        find.text('本人记忆 1'),
        -240,
        maxScrolls: 100,
        scrollable: find.byType(Scrollable).first,
      );
      expect(find.text('本人记忆 1'), findsOneWidget);
      expect(more, 1);
      expect(t.takeException(), isNull);
      await t.pumpWidget(const SizedBox());
    },
  );
}
