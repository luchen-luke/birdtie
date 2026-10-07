import 'dart:async';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:birdtie_client/src/workspace/business_knowledge_controller.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'business_console_controller_test.dart'
    show merchant, person, view, reply;
import 'business_knowledge_api_test.dart' show knowledgeAnswer, knowledgePlace;

class KnowledgeFixture {
  KnowledgeFixture(
    FutureOr<http.Response> Function(http.Request) handler, {
    DateTime Function()? now,
  }) {
    api = BusinessApi(
      authorizationHeader: () => token,
      apiBaseUrl: 'http://fixture',
      client: MockClient((r) async => handler(r)),
    );
    c = BusinessKnowledgeController(
      api: api,
      businessID: merchant,
      accountID: () => owner,
      authorizationHeader: () => token,
      workspaceID: () => workspace,
      currentBusinessID: () => selected,
      bindingCurrent: () => binding,
      sourceFrame: () => source,
      identityChanges: changes,
      now: now,
    );
  }
  String? token = 'Bearer local',
      owner = person,
      workspace,
      selected = merchant;
  bool binding = true;
  Object source = Object();
  final changes = ChangeNotifier();
  late final BusinessApi api;
  late final BusinessKnowledgeController c;
  void changed() {
    changes.notifyListeners();
  }

  void dispose() {
    c.dispose();
    changes.dispose();
    api.dispose();
  }
}

void main() {
  test(
    'borrowed transport with another session cannot read on behalf of current person',
    () async {
      var calls = 0;
      final changes = ChangeNotifier();
      final api = BusinessApi(
        authorizationHeader: () => 'Bearer another',
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          calls++;
          return reply(view());
        }),
      );
      final source = Object();
      final c = BusinessKnowledgeController(
        api: api,
        businessID: merchant,
        accountID: () => person,
        authorizationHeader: () => 'Bearer current',
        workspaceID: () => null,
        currentBusinessID: () => merchant,
        bindingCurrent: () => true,
        sourceFrame: () => source,
        identityChanges: changes,
      );
      await c.load();
      expect(calls, 0);
      expect(c.permitted, isFalse);
      expect(c.businessName, isEmpty);
      c.dispose();
      changes.dispose();
      api.dispose();
    },
  );
  test(
    'load minimizes venues, no question POST until explicitly selected and asked',
    () async {
      var posts = 0;
      final s = view();
      s['venues'] = [
        {
          'placeId': knowledgePlace,
          'placeName': '明确场地',
          'operationStatus': 'verified',
          'rightsNote': 'PRIVATE',
          'sourceUrl': 'PRIVATE',
          'facts': {'note': 'PRIVATE'},
        },
        {'placeId': person, 'placeName': '未核验场地', 'operationStatus': 'pending'},
      ];
      final f = KnowledgeFixture((r) {
        if (r.method == 'POST') {
          posts++;
          return reply(knowledgeAnswer(venue: true));
        }
        return reply(s);
      });
      addTearDown(f.dispose);
      await f.c.load();
      expect(f.c.venues.map((v) => v.name), ['明确场地']);
      expect(posts, 0);
      f.c.selectQuestion('场地适用场景');
      await f.c.ask();
      expect(posts, 0);
      f.c.selectPlace(knowledgePlace);
      await f.c.ask();
      expect(posts, 1);
      expect(f.c.answer?.status, 'known');
      f.c.selectQuestion('商家介绍');
      expect(f.c.answer, isNull);
      expect(f.c.placeID, isNull);
    },
  );
  for (final event in [
    'account',
    'session',
    'organization',
    'business',
    'frame',
    'role',
    'binding',
  ]) {
    test(
      'current $event change clears answer and cannot revive after ABA',
      () async {
        final f = KnowledgeFixture(
          (r) => reply(r.method == 'GET' ? view() : knowledgeAnswer()),
        );
        addTearDown(f.dispose);
        await f.c.load();
        f.c.selectQuestion('商家介绍');
        await f.c.ask();
        expect(f.c.answer, isNotNull);
        switch (event) {
          case 'account':
            f.owner = knowledgePlace;
          case 'session':
            f.token = 'Bearer new';
          case 'organization':
            f.workspace = knowledgePlace;
          case 'business':
            f.selected = knowledgePlace;
          case 'frame':
            f.source = Object();
          case 'role':
            f.binding = false;
          case 'binding':
            f.binding = false;
        }
        f.changed();
        expect(f.c.answer, isNull);
        expect(f.c.venues, isEmpty);
        f.owner = person;
        f.token = 'Bearer local';
        f.workspace = null;
        f.selected = merchant;
        f.binding = true;
        f.changed();
        await f.c.load();
        expect(f.c.permitted, isFalse);
        expect(f.c.answer, isNull);
      },
    );
  }
  test('late old query ignored even same final token after ABA', () async {
    final pending = Completer<http.Response>();
    final f = KnowledgeFixture(
      (r) => r.method == 'GET' ? reply(view()) : pending.future,
    );
    addTearDown(f.dispose);
    await f.c.load();
    f.c.selectQuestion('营业时间');
    final ask = f.c.ask();
    await Future<void>.delayed(Duration.zero);
    f.token = 'Bearer next';
    f.changed();
    f.token = 'Bearer local';
    f.changed();
    pending.complete(reply(knowledgeAnswer()));
    await ask;
    expect(f.c.answer, isNull);
    expect(f.c.permitted, isFalse);
  });
  test(
    'fresh native role revocation prevents POST and removes old business data',
    () async {
      bool denied = false;
      var posts = 0;
      final f = KnowledgeFixture((r) {
        if (r.method == 'POST') {
          posts++;
          return reply(knowledgeAnswer());
        }
        return reply(view(manage: !denied));
      });
      addTearDown(f.dispose);
      await f.c.load();
      f.c.selectQuestion('商家介绍');
      await f.c.ask();
      expect(posts, 1);
      denied = true;
      await f.c.ask();
      expect(posts, 1);
      expect(f.c.answer, isNull);
      expect(f.c.businessName, isEmpty);
      expect(f.c.ready, isFalse);
    },
  );
  test(
    'native query 403 clears prior answer; safe readonly retry requires current read',
    () async {
      var denied = false;
      final f = KnowledgeFixture(
        (r) => reply(
          r.method == 'GET' ? view() : knowledgeAnswer(),
          r.method == 'POST' && denied ? 403 : 200,
        ),
      );
      addTearDown(f.dispose);
      await f.c.load();
      f.c.selectQuestion('营业时间');
      await f.c.ask();
      denied = true;
      await f.c.ask();
      expect(f.c.answer, isNull);
      expect(f.c.ready, isFalse);
      expect(f.c.error, contains('权限'));
    },
  );
  testWidgets('natural source expiry removes answer with no automatic POST', (
    tester,
  ) async {
    final base = DateTime.utc(2026, 10, 4);
    var at = base, posts = 0;
    final f = KnowledgeFixture((r) {
      if (r.method == 'POST') {
        posts++;
        return reply(
          knowledgeAnswer(
            until: base.add(const Duration(seconds: 2)).toIso8601String(),
          ),
        );
      }
      return reply(view());
    }, now: () => at);
    await f.c.load();
    f.c.selectQuestion('营业时间');
    await f.c.ask();
    expect(f.c.answer, isNotNull);
    at = base.add(const Duration(seconds: 2));
    await tester.pump(const Duration(seconds: 2));
    expect(f.c.answer, isNull);
    expect(f.c.error, contains('到期'));
    expect(posts, 1);
    f.dispose();
  });
  test(
    'disposed controller rejects delayed result and never disposes borrowed transport',
    () async {
      final pending = Completer<http.Response>();
      final f = KnowledgeFixture(
        (r) => r.method == 'POST' ? pending.future : reply(view()),
      );
      await f.c.load();
      f.c.selectQuestion('营业时间');
      final ask = f.c.ask();
      await Future<void>.delayed(Duration.zero);
      f.c.dispose();
      pending.complete(reply(knowledgeAnswer()));
      await ask;
      expect(f.c.answer, isNull);
      expect((await f.api.read(merchant)).canManage, isTrue);
      f.changes.dispose();
      f.api.dispose();
    },
  );
}
