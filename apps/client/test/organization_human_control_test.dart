import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;
import 'package:birdtie_client/src/workspace/organization_human_control.dart';
import 'package:birdtie_client/src/workspace/organization_membership_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'batch_a_visual_test.dart' show visualTheme;

const orgTestID = '11111111-1111-4111-8111-111111111111';
const orgTestOwner = '22222222-2222-4222-8222-222222222222';
const orgTestTarget = '33333333-3333-4333-8333-333333333333';
const orgTestMember = '44444444-4444-4444-8444-444444444444';
const orgTestProposal = '55555555-5555-4555-8555-555555555555';
const orgTestOp = '66666666-6666-4666-8666-666666666666';
const orgTestBase = 'http://127.0.0.1:18113';
const orgCaptureKey = Key('organization-isolated-render');
http.Response orgWire(Object? data, {int status = 200}) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);
Map<String, dynamic> orgMemberWire({
  String id = orgTestMember,
  String org = orgTestID,
  String account = orgTestTarget,
  String? name = '合成成员',
  String role = 'member',
  String status = 'active',
  int version = 2,
  int authority = 4,
}) => {
  'id': id,
  'organizationId': org,
  'userAccountId': account,
  if (name != null) 'displayName': name,
  'role': role,
  'status': status,
  'version': version,
  'authorityVersion': authority,
};
Map<String, dynamic> orgRosterWire({
  String org = orgTestID,
  String role = 'owner',
  int authority = 4,
  List<Map<String, dynamic>>? members,
  int lease = 25,
}) => {
  'organizationId': org,
  'role': role,
  'authorityVersion': authority,
  'members': members ?? [orgMemberWire(authority: authority)],
  'observedAt': DateTime.now().toUtc().toIso8601String(),
  'expiresAt': DateTime.now()
      .toUtc()
      .add(Duration(seconds: lease))
      .toIso8601String(),
};
OrganizationControlInput orgInput(Map<String, dynamic> b) =>
    OrganizationControlInput(
      action: b['action'],
      operationID: b['operationId'],
      expectedAuthorityVersion: b['expectedAuthorityVersion'],
      userAccountID: b['userAccountId'] ?? '',
      membershipID: b['membershipId'] ?? '',
      role: b['role'] ?? '',
      expectedMemberVersion: b['expectedMemberVersion'] ?? 0,
      cityID: b['cityId'] ?? '',
      latitude: (b['latitude'] as num?)?.toDouble() ?? 0,
      longitude: (b['longitude'] as num?)?.toDouble() ?? 0,
      proposalID: b['proposalId'] ?? '',
      expectedProposalVersion: b['expectedProposalVersion'] ?? 0,
      expectedCanonicalRevision: b['expectedCanonicalRevision'] ?? 0,
    );
Map<String, dynamic> orgPreviewWire(
  OrganizationControlInput input, {
  String org = orgTestID,
  int? canonical,
  int lease = 15,
}) => {
  'organizationId': org,
  'operationId': input.operationID,
  'action': input.action,
  'approval': 'oh1.1.2.${'a' * 64}',
  'expiresAt': DateTime.now()
      .toUtc()
      .add(Duration(seconds: lease))
      .toIso8601String(),
  'inputSha256': input.digest(org),
  'authorityVersion': input.expectedAuthorityVersion,
  'memberVersion': input.expectedMemberVersion,
  'proposalVersion': input.expectedProposalVersion,
  'canonicalRevision': canonical ?? input.expectedCanonicalRevision,
};
Map<String, dynamic> orgReceiptWire(
  OrganizationControlSelector s, {
  bool stopped = false,
  int? canonical,
}) => {
  'organizationId': s.organizationID,
  'operationId': s.operationID,
  'action': stopped ? 'STOP' : s.action,
  'status': stopped ? 'STOPPED' : 'SUCCEEDED',
  'authorityVersion': stopped
      ? 0
      : s.expectedAuthorityVersion + (s.memberAction ? 1 : 0),
  'memberVersion': stopped
      ? 0
      : s.memberAction
      ? s.expectedMemberVersion + 1
      : 0,
  'proposalVersion': stopped
      ? 0
      : s.memberAction || s.action == 'HIDE_POINT'
      ? 0
      : s.action == 'PROPOSE'
      ? 1
      : s.expectedProposalVersion + 1,
  'canonicalRevision': stopped
      ? 0
      : s.memberAction
      ? 0
      : canonical ??
            s.expectedCanonicalRevision +
                (const {'APPROVE_POINT', 'HIDE_POINT'}.contains(s.action)
                    ? 1
                    : 0),
  if (!stopped && s.memberAction)
    'membershipId': s.action == 'INVITE' && s.expectedMemberVersion == 0
        ? s.operationID
        : s.membershipID,
  if (!stopped && !s.memberAction && s.action != 'HIDE_POINT')
    'proposalId': s.action == 'PROPOSE' ? s.operationID : s.proposalID,
};
Future<void> orgSettle(WidgetTester t) async {
  // Journal preparation, original receipt and current-source GET are separate
  // streamed MockClient stages. Drain each before settling fake animations.
  for (var i = 0; i < 3; i++) {
    await t.pump();
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 60)),
    );
  }
  await t.pumpAndSettle();
}

Future<void> orgTap(WidgetTester t, Finder f) async {
  await t.ensureVisible(f);
  await t.tap(f);
  await orgSettle(t);
}

Future<void> orgFonts() async {
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
}

Widget orgHarness(Widget child) => RepaintBoundary(
  key: orgCaptureKey,
  child: MaterialApp(
    debugShowCheckedModeBanner: false,
    theme: visualTheme(),
    home: Column(
      children: [
        Expanded(child: child),
        const Material(
          child: SafeArea(
            top: false,
            child: Text(
              '隔离测试渲染 · 合成人物和组织 · 非真实邀请',
              style: TextStyle(fontFamily: 'BatchACJK', fontSize: 11),
            ),
          ),
        ),
      ],
    ),
  ),
);
Future<void> orgCapture(WidgetTester t, String name) async {
  final directory = Platform.environment['BIRDTIE_ORG_UI_OUTPUT'];
  if (directory == null) return;
  await t.runAsync(() async {
    final image = await t
        .renderObject<RenderRepaintBoundary>(find.byKey(orgCaptureKey))
        .toImage(pixelRatio: 1);
    final bytes = await image.toByteData(format: ui.ImageByteFormat.png);
    final f = File('$directory/$name.png');
    await f.parent.create(recursive: true);
    if (await f.exists()) throw StateError('Immutable screenshot exists');
    await f.writeAsBytes(bytes!.buffer.asUint8List());
    image.dispose();
  });
}

class OrgFailBackStore extends MemoryOrganizationControlPendingStore {
  bool failBack = false;
  @override
  Future<OrganizationControlPending?> read(
    String o,
    String owner,
    String org,
  ) async {
    if (failBack) {
      failBack = false;
      throw StateError('injected readback failure');
    }
    return super.read(o, owner, org);
  }

  @override
  Future<void> write(
    String o,
    String owner,
    String org,
    OrganizationControlPending p,
  ) async {
    await super.write(o, owner, org, p);
    if (p.stage == 'UNKNOWN') failBack = true;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const invite = OrganizationControlInput(
    action: 'INVITE',
    operationID: orgTestOp,
    expectedAuthorityVersion: 4,
    userAccountID: orgTestTarget,
    role: 'member',
  );
  test(
    '139 closed body, IEEE754 negative zero and input changes bind digest',
    () {
      const p = OrganizationControlInput(
        action: 'PROPOSE',
        operationID: orgTestOp,
        expectedAuthorityVersion: 4,
        cityID: 'aberdeen-gb',
        latitude: -0.0,
        longitude: 180,
        expectedCanonicalRevision: 2,
      );
      const positive = OrganizationControlInput(
        action: 'PROPOSE',
        operationID: orgTestOp,
        expectedAuthorityVersion: 4,
        cityID: 'aberdeen-gb',
        latitude: 0.0,
        longitude: 180,
        expectedCanonicalRevision: 2,
      );
      expect(p.valid, isTrue);
      expect(p.digest(orgTestID), positive.digest(orgTestID));
      expect(p.toJson().keys.toSet(), {
        'action',
        'operationId',
        'expectedAuthorityVersion',
        'cityId',
        'latitude',
        'longitude',
        'expectedCanonicalRevision',
      });
      expect(invite.digest(orgTestID), isNot(p.digest(orgTestID)));
      expect(
        const OrganizationControlInput(
          action: 'INVITE',
          operationID: orgTestOp,
          expectedAuthorityVersion: 4,
          userAccountID: orgTestTarget,
          role: 'owner',
        ).valid,
        isFalse,
      );
    },
  );
  test(
    '139 approval binds original input and ignores unused current canonical for membership',
    () {
      final data = orgPreviewWire(invite, canonical: 8);
      expect(
        OrganizationControlPreview.fromJson(
          data,
          orgTestID,
          invite,
        ).canonicalRevision,
        8,
      );
      for (final changed in [
        {...data, 'memberVersion': 2},
        {...data, 'inputSha256': 'b' * 64},
        {...data, 'organizationId': orgTestTarget},
        {...data, 'extra': true},
      ]) {
        expect(
          () => OrganizationControlPreview.fromJson(changed, orgTestID, invite),
          throwsFormatException,
        );
      }
    },
  );
  test(
    '139 immutable receipt targets original versions, STOP zeros, withdrawal current axis',
    () {
      final s = invite.selector(orgTestID);
      expect(
        OrganizationControlReceipt.fromJson(orgReceiptWire(s), s).membershipID,
        orgTestOp,
      );
      expect(
        () => OrganizationControlReceipt.fromJson({
          ...orgReceiptWire(s),
          'authorityVersion': 6,
        }, s),
        throwsFormatException,
      );
      expect(
        OrganizationControlReceipt.fromJson(
          orgReceiptWire(s, stopped: true),
          s,
        ).status,
        'STOPPED',
      );
      expect(
        () => OrganizationControlReceipt.fromJson({
          ...orgReceiptWire(s, stopped: true),
          'membershipId': orgTestMember,
        }, s),
        throwsFormatException,
      );
      const withdrawal = OrganizationControlInput(
        action: 'WITHDRAW_POINT',
        operationID: orgTestOp,
        expectedAuthorityVersion: 4,
        proposalID: orgTestProposal,
        expectedProposalVersion: 2,
      );
      expect(
        OrganizationControlReceipt.fromJson(
          orgReceiptWire(withdrawal.selector(orgTestID), canonical: 9),
          withdrawal.selector(orgTestID),
        ).canonicalRevision,
        9,
      );
    },
  );
  test(
    '139 roster missing displayName retains member and authority; strict version and duplicate reject',
    () {
      final data = orgRosterWire(members: [orgMemberWire(name: null)]);
      final s = OrganizationMembersControl.fromJson(data, orgTestID);
      expect(s.members.single.displayName, '');
      expect(s.canChange(s.members.single), isTrue);
      expect(
        () => OrganizationMembersControl.fromJson({
          ...data,
          'members': [orgMemberWire(name: null), orgMemberWire(name: null)],
        }, orgTestID),
        throwsFormatException,
      );
      expect(
        () => OrganizationMember.fromJson({...orgMemberWire(), 'version': 2.0}),
        throwsFormatException,
      );
    },
  );
  test(
    '139 unknown result restart GET404 keeps original selector and never retries PUT',
    () async {
      var puts = 0, gets = 0;
      final store = MemoryOrganizationControlPendingStore();
      final api = OrganizationHumanControlApi(
        client: MockClient((r) async {
          if (r.url.path.endsWith('/preview'))
            return orgWire(orgPreviewWire(orgInput(jsonDecode(r.body))));
          if (r.method == 'PUT') {
            puts++;
            return orgWire(null, status: 401);
          }
          gets++;
          return orgWire(null, status: 404);
        }),
        baseUrl: orgTestBase,
        authorizationHeader: () => 'Bearer original',
      );
      OrganizationControlOperation make() => OrganizationControlOperation(
        api: api,
        organizationID: orgTestID,
        ownerID: () => orgTestOwner,
        current: () => true,
        store: store,
      );
      final op = make();
      await op.restore();
      final p = (await op.preview(invite))!;
      expect(
        await op.submit(
          p,
          invite.selector(orgTestID),
          sourceCurrent: () => true,
        ),
        isNull,
      );
      expect(puts, 1);
      final journal = jsonEncode(store.values.values.single.toJson());
      for (final forbidden in [
        'Bearer',
        'userAccountId',
        'displayName',
        'content',
        'latitude',
        'role',
      ]) {
        expect(journal.contains(forbidden), isFalse);
      }
      op.dispose();
      final restarted = make();
      await restarted.restore();
      expect(restarted.canWrite, isFalse);
      expect(await restarted.recover(), isNull);
      expect(puts, 1);
      expect(gets, 1);
      expect(restarted.pending!.selector.operationID, orgTestOp);
      restarted.dispose();
    },
  );
  test(
    '139 successful receipt ends journal independently of lost current authority',
    () async {
      final s = invite.selector(orgTestID),
          store = MemoryOrganizationControlPendingStore();
      await store.write(
        orgTestBase,
        orgTestOwner,
        orgTestID,
        OrganizationControlPending(s),
      );
      var writes = 0;
      final api = OrganizationHumanControlApi(
        client: MockClient((r) async {
          if (r.method == 'PUT') writes++;
          return orgWire(orgReceiptWire(s));
        }),
        baseUrl: orgTestBase,
        authorizationHeader: () => 'Bearer original',
      );
      final op = OrganizationControlOperation(
        api: api,
        organizationID: orgTestID,
        ownerID: () => orgTestOwner,
        current: () => true,
        store: store,
      );
      await op.restore();
      expect((await op.recover())!.status, 'SUCCEEDED');
      expect(store.values, isEmpty);
      expect(op.receipt!.memberVersion, 1);
      expect(writes, 0);
      op.dispose();
    },
  );
  test(
    '139 failed pre-PUT readback is local preparation, safe repair cannot replay',
    () async {
      var puts = 0;
      final store = OrgFailBackStore(),
          api = OrganizationHumanControlApi(
            client: MockClient((r) async {
              if (r.method == 'PUT') puts++;
              return orgWire(orgPreviewWire(invite));
            }),
            baseUrl: orgTestBase,
            authorizationHeader: () => 'Bearer original',
          );
      final op = OrganizationControlOperation(
        api: api,
        organizationID: orgTestID,
        ownerID: () => orgTestOwner,
        current: () => true,
        store: store,
      );
      await op.restore();
      final p = (await op.preview(invite))!;
      await op.submit(p, invite.selector(orgTestID), sourceCurrent: () => true);
      expect(puts, 0);
      expect(op.locallyPrepared, isTrue);
      await op.repairPrepared();
      expect(store.values, isEmpty);
      expect(op.canWrite, isTrue);
      expect(puts, 0);
      op.dispose();
    },
  );
  test(
    '139 late response owner and target ABA is permanently retired',
    () async {
      final delayed = Completer<http.Response>(), signal = ValueNotifier(0);
      var owner = orgTestOwner, target = orgTestID;
      final api = OrganizationHumanControlApi(
        client: MockClient((r) => delayed.future),
        baseUrl: orgTestBase,
        authorizationHeader: () => 'Bearer same',
      );
      final binding = OrganizationHumanPageBinding(
        api: api,
        target: () => target,
        header: () => 'Bearer same',
        ownerID: () => owner,
        sourceCurrent: () => true,
        identityChanges: signal,
        onRetired: (_) {},
      );
      await binding.resolveOwner();
      await binding.restore(orgTestID);
      final op = binding.operation(orgTestID), future = op.preview(invite);
      owner = orgTestTarget;
      target = orgTestTarget;
      signal.value++;
      owner = orgTestOwner;
      target = orgTestID;
      signal.value++;
      delayed.complete(orgWire(orgPreviewWire(invite)));
      expect(await future, isNull);
      expect(op.pending, isNull);
      expect(op.canWrite, isFalse);
      binding.dispose();
      signal.dispose();
    },
  );
  test(
    '139 same owner credential renewal retains scope draft but retires approval',
    () async {
      final signal = ValueNotifier(0);
      var token = 'Bearer first';
      final events = <bool>[];
      final api = OrganizationHumanControlApi(
        client: MockClient((r) async => orgWire(null)),
        baseUrl: orgTestBase,
        authorizationHeader: () => token,
      );
      final b = OrganizationHumanPageBinding(
        api: api,
        target: () => orgTestID,
        header: () => token,
        ownerID: () => orgTestOwner,
        sourceCurrent: () => true,
        identityChanges: signal,
        onRetired: events.add,
      );
      final old = b.capture();
      token = 'Bearer renewed';
      signal.value++;
      expect(old(), isFalse);
      expect(events, [false]);
      expect(b.current, isTrue);
      b.dispose();
      signal.dispose();
    },
  );
  test(
    '139 secure cross-instance locked record rejects overwrite and precise deletion',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final a = SecureOrganizationControlPendingStore(),
          b = SecureOrganizationControlPendingStore();
      final p = OrganizationControlPending(invite.selector(orgTestID));
      await a.write(orgTestBase, orgTestOwner, orgTestID, p);
      final other = OrganizationControlPending(
        const OrganizationControlInput(
          action: 'REVOKE',
          operationID: orgTestProposal,
          expectedAuthorityVersion: 4,
          membershipID: orgTestMember,
          expectedMemberVersion: 2,
        ).selector(orgTestID),
      );
      await expectLater(
        b.write(orgTestBase, orgTestOwner, orgTestID, other),
        throwsFormatException,
      );
      await expectLater(
        b.remove(orgTestBase, orgTestOwner, orgTestID, other),
        throwsFormatException,
      );
      expect(
        (await b.read(orgTestBase, orgTestOwner, orgTestID))!.same(p),
        isTrue,
      );
      expect(await b.organizations(orgTestBase, orgTestOwner), [orgTestID]);
      await b.remove(orgTestBase, orgTestOwner, orgTestID, p);
      expect(await a.organizations(orgTestBase, orgTestOwner), isEmpty);
    },
  );
}
