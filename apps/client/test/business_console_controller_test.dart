import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/business_api.dart';
import 'package:birdtie_client/src/workspace/business_console_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const merchant = 'be000000-0000-4000-8000-000000000001';
const person = 'be000000-0000-4000-8000-000000000002';
Map<String, dynamic> view({
  bool manage = true,
  bool owner = true,
  int version = 0,
}) => {
  'business': {
    'id': merchant,
    'name': '本地合成商家',
    'claimStatus': 'verified',
    'role': owner ? 'owner' : 'admin',
  },
  'canManage': manage,
  'canManageMembers': owner,
  'canReview': false,
  'reviewPermissions': <String>[],
  'membershipVersion': version,
  'claim': {'version': 2, 'state': 'verified', 'submittedBy': person},
  'profile': null,
  'venues': <dynamic>[],
  'members': <dynamic>[],
};
http.Response reply(dynamic data, [int status = 200]) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), status);
BusinessDraft memberDraft() => BusinessDraft(
  kind: 'member',
  body: {
    'expectedVersion': 0,
    'targetPersonId': person,
    'action': 'grant',
    'role': 'admin',
  },
);

class Fixture {
  Fixture(FutureOr<http.Response> Function(http.Request) handler) {
    api = BusinessApi(
      authorizationHeader: () => token,
      client: MockClient((r) async => handler(r)),
      apiBaseUrl: 'http://127.0.0.1:1',
    );
    controller = BusinessConsoleController(
      api: api,
      accountID: () => account,
      authorizationHeader: () => token,
      organizationWorkspaceID: () => organization,
    );
  }
  String? token = 'Bearer contract-owner', account = person, organization;
  late final BusinessApi api;
  late final BusinessConsoleController controller;
  void dispose() {
    controller.dispose();
    api.dispose();
  }
}

void main() {
  test(
    'List and selection are reads; preview cancel produces no write',
    () async {
      final methods = <String>[];
      final f = Fixture((r) {
        methods.add(r.method);
        return reply(
          r.url.path.endsWith('/console') ? view() : [view()['business']],
        );
      });
      addTearDown(f.dispose);
      await f.controller.load();
      await f.controller.select(merchant);
      f.controller.edit(memberDraft());
      final approval = f.controller.preview();
      expect(approval, isNotNull);
      f.controller.cancelPreview();
      await f.controller.submit(approval!);
      expect(methods, ['GET', 'GET']);
    },
  );
  test(
    'Specific immutable approval is single use and duplicate clicks do not write twice',
    () async {
      var writes = 0;
      final pending = Completer<http.Response>();
      final f = Fixture((r) {
        if (r.method == 'PUT') {
          writes++;
          return pending.future;
        }
        return reply(view(version: writes));
      });
      addTearDown(f.dispose);
      await f.controller.select(merchant);
      final original = {
        'expectedVersion': 0,
        'targetPersonId': person,
        'action': 'grant',
        'role': 'admin',
      };
      f.controller.edit(BusinessDraft(kind: 'member', body: original));
      original['role'] = 'member';
      final approval = f.controller.preview()!;
      expect(approval.draft.body['role'], 'admin');
      final first = f.controller.submit(approval);
      await Future<void>.delayed(Duration.zero);
      await f.controller.submit(approval);
      expect(writes, 1);
      pending.complete(reply(view(version: 1)));
      await first;
      await f.controller.submit(approval);
      expect(writes, 1);
      expect(f.controller.snapshot?.membershipVersion, 1);
    },
  );
  test('Editing a draft invalidates its old approval', () async {
    var writes = 0;
    final f = Fixture((r) {
      if (r.method != 'GET') writes++;
      return reply(view());
    });
    addTearDown(f.dispose);
    await f.controller.select(merchant);
    f.controller.edit(memberDraft());
    final old = f.controller.preview()!;
    f.controller.edit(memberDraft());
    await f.controller.submit(old);
    expect(writes, 0);
  });
  test(
    'Account token and organization ABA invalidate approval even after returning',
    () async {
      for (final axis in ['account', 'token', 'organization']) {
        var writes = 0;
        final f = Fixture((r) {
          if (r.method != 'GET') writes++;
          return reply(view());
        });
        await f.controller.select(merchant);
        f.controller.edit(memberDraft());
        final old = f.controller.preview()!;
        if (axis == 'account') f.account = merchant;
        if (axis == 'token') f.token = 'Bearer other';
        if (axis == 'organization') f.organization = merchant;
        f.controller.synchronizeIdentity();
        f.account = person;
        f.token = 'Bearer contract-owner';
        f.organization = null;
        f.controller.synchronizeIdentity();
        await f.controller.select(merchant);
        f.controller.edit(old.draft);
        await f.controller.submit(old);
        expect(writes, 0);
        f.dispose();
      }
    },
  );
  test(
    'Late read and ABA do not restore the previous identity snapshot',
    () async {
      final delayed = Completer<http.Response>();
      final f = Fixture((_) => delayed.future);
      addTearDown(f.dispose);
      final read = f.controller.select(merchant);
      f.organization = merchant;
      f.controller.synchronizeIdentity();
      f.organization = null;
      f.controller.synchronizeIdentity();
      delayed.complete(reply(view()));
      await read;
      expect(f.controller.snapshot, isNull);
      expect(f.controller.selectedID, isNull);
    },
  );
  test('Admin and stale version cannot approve membership writes', () async {
    var writes = 0;
    final f = Fixture((r) {
      if (r.method != 'GET') writes++;
      return reply(view(owner: false));
    });
    addTearDown(f.dispose);
    await f.controller.select(merchant);
    f.controller.edit(memberDraft());
    expect(f.controller.preview(), isNull);
    expect(writes, 0);
    f.controller.snapshot = BusinessConsoleSnapshot.fromJson(view(version: 2));
    f.controller.edit(memberDraft());
    expect(f.controller.preview(), isNull);
  });
  test(
    'Unknown write actually reads current status and never resends',
    () async {
      var writes = 0, reads = 0;
      final f = Fixture((r) {
        if (r.method == 'PUT') {
          writes++;
          return reply({}, 503);
        }
        reads++;
        return reply(view(version: reads == 1 ? 0 : 1));
      });
      addTearDown(f.dispose);
      await f.controller.select(merchant);
      f.controller.edit(memberDraft());
      await f.controller.submit(f.controller.preview()!);
      expect(writes, 1);
      expect(reads, 2);
      expect(f.controller.uncertain, isFalse);
      expect(f.controller.snapshot?.membershipVersion, 1);
      expect(f.controller.message, contains('未自动重发'));
    },
  );
  test(
    'Unresolved result disables further approval; explicit fresh read recovers',
    () async {
      var writes = 0, reads = 0;
      var recover = false;
      final f = Fixture((r) {
        if (r.method == 'PUT') {
          writes++;
          return reply({}, 503);
        }
        reads++;
        if (reads > 1 && !recover) return reply({}, 403);
        return reply(view());
      });
      addTearDown(f.dispose);
      await f.controller.select(merchant);
      f.controller.edit(memberDraft());
      await f.controller.submit(f.controller.preview()!);
      expect(f.controller.uncertain, isTrue);
      expect(f.controller.snapshot, isNull);
      f.controller.edit(memberDraft());
      expect(f.controller.preview(), isNull);
      expect(writes, 1);
      recover = true;
      await f.controller.refresh();
      expect(f.controller.uncertain, isFalse);
      expect(f.controller.message, contains('不证明'));
    },
  );
  test('Org mode and anonymous users cannot even load', () async {
    var calls = 0;
    final f = Fixture((_) {
      calls++;
      return reply([]);
    });
    addTearDown(f.dispose);
    f.organization = merchant;
    await f.controller.load();
    f.controller.newClaim();
    expect(f.controller.selectedID, isNull);
    f.organization = null;
    f.token = null;
    await f.controller.load();
    expect(calls, 0);
  });
  test(
    'Fresh proposal ID is canonical and does not create a server identity',
    () {
      var calls = 0;
      final f = Fixture((_) {
        calls++;
        return reply([]);
      });
      addTearDown(f.dispose);
      f.controller.newClaim();
      expect(BusinessApi.validID(f.controller.selectedID!), isTrue);
      expect(calls, 0);
      f.controller.edit(BusinessDraft(kind: 'claim', body: {'name': '合成商家'}));
      expect(f.controller.preview(), isNotNull);
    },
  );
}
