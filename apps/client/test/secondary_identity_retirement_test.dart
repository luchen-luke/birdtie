import 'human_causal_control_test.dart';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/content/private_moment_controller.dart';
import 'package:birdtie_client/src/workspace/organization_activity_api.dart';
import 'package:birdtie_client/src/workspace/organization_console.dart';
import 'package:birdtie_client/src/workspace/organization_faq_page.dart';
import 'package:birdtie_client/src/workspace/organization_workspaces.dart';
import 'package:birdtie_client/src/workspace/settings_page.dart';
import 'package:birdtie_client/src/workspace/support_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'now_scope_recovery_test.dart' show NowFixtureCity;
import 'entity_cover_entry_test.dart' show managedCoveredActivity;

const _owner = '91111111-1111-4111-8111-111111111111';
const _org = '92222222-2222-4222-8222-222222222222';
const _base = 'http://secondary-owned.test';

class _Wire extends http.BaseClient {
  final requests =
      <({String method, String path, String? token, String body})>[];
  Completer<http.StreamedResponse>? reportRead, reportWrite, faqRead;
  Completer<http.StreamedResponse>? activityRead;
  List<Map<String, dynamic>> activities = const [];
  bool closed = false;
  HumanReportInput? reportInput;
  http.StreamedResponse response(Object data, [int code = 200]) =>
      http.StreamedResponse(
        Stream.value(utf8.encode(jsonEncode({'data': data}))),
        code,
        headers: {'content-type': 'application/json'},
      );
  @override
  Future<http.StreamedResponse> send(http.BaseRequest r) {
    requests.add((
      method: r.method,
      path: r.url.path,
      token: r.headers['Authorization'],
      body: r is http.Request ? r.body : '',
    ));
    if (r.url.path.endsWith('/activities') && r.method == 'GET') {
      final pending = activityRead;
      activityRead = null;
      return pending?.future ?? Future.value(response(activities));
    }
    final lease = {
      'observedAt': DateTime.now().toUtc().toIso8601String(),
      'expiresAt': DateTime.now()
          .toUtc()
          .add(const Duration(seconds: 25))
          .toIso8601String(),
    };
    if (r.url.path.endsWith('/faq-control') && r.method == 'GET')
      return Future.value(
        response({
          'organizationId': _org,
          'role': 'owner',
          'items': [],
          ...lease,
        }),
      );
    if (r.url.path == '/v1/me/report-control' && r.method == 'GET')
      return Future.value(
        response({'remainingQuota': 5, 'canSubmit': true, ...lease}),
      );
    if (r.url.path.endsWith('/previews')) {
      final b = jsonDecode((r as http.Request).body) as Map<String, dynamic>;
      final f = r.url.path.contains('/faq-control')
          ? HumanCausalFamily.faq
          : HumanCausalFamily.report;
      final input = humanInput(f, b, org: _org);
      if (input is HumanReportInput) reportInput = input;
      return Future.value(response(humanPreviewWire(input)));
    }
    if (r.method == 'PUT' && r.url.path.contains('/operations/')) {
      if (reportWrite != null && r.url.path.contains('/report-control/'))
        return reportWrite!.future;
      final b = jsonDecode((r as http.Request).body) as Map<String, dynamic>;
      return Future.value(
        response(
          humanReceiptWire(
            humanInput(
              r.url.path.contains('/faq-control')
                  ? HumanCausalFamily.faq
                  : HumanCausalFamily.report,
              b,
              org: _org,
            ),
          ),
        ),
      );
    }
    if (r.url.path == '/v1/me/reports') {
      if (r.method == 'GET' && reportRead != null) return reportRead!.future;
      return Future.value(response([]));
    }
    return Future.value(response([]));
  }

  @override
  void close() {
    closed = true;
    super.close();
  }
}

Future<void> _settle(WidgetTester t) async {
  // Pump post-frame initializers, then let real secure-storage/stream events run.
  for (var i = 0; i < 4; i++) {
    await t.pump(const Duration(milliseconds: 180));
    await t.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 25)),
    );
  }
  await t.pump(const Duration(milliseconds: 180));
}

Future<void> _tap(WidgetTester t, String text) async {
  final f = find.text(text);
  await t.scrollUntilVisible(
    f,
    180,
    maxScrolls: 50,
    scrollable: find.byType(Scrollable).last,
  );
  await t.ensureVisible(f);
  expect(f.hitTestable(), findsOneWidget);
  await t.tap(f);
  await _settle(t);
}

void _normal(WidgetTester t) {
  t.view.physicalSize = const Size(390, 844);
  t.view.devicePixelRatio = 1;
  addTearDown(t.view.resetPhysicalSize);
  addTearDown(t.view.resetDevicePixelRatio);
}

void main() {
  setUp(() => FlutterSecureStorage.setMockInitialValues({}));
  for (final scenario in ['lateActivityListABA', 'oldPublishDialogABA']) {
    testWidgets(
      'secondary actual organization console $scenario never uses retired private state',
      (t) async {
        _normal(t);
        final auth = SeedTestAuth()
          ..owner = _owner
          ..token = 'Bearer synthetic-A';
        final city = NowFixtureCity(), client = _Wire();
        final api = OrganizationActivityApi(
          client: client,
          apiBaseUrl: _base,
          authorizationHeader: () => auth.authorizationHeader,
        );
        final orgs = OrganizationWorkspaceController(
          client: client,
          apiBaseUrl: _base,
          authorizationHeader: () => auth.authorizationHeader,
        );
        OrganizationWorkspace org(String role) => OrganizationWorkspace(
          id: _org,
          name: '隔离合成组织',
          role: role,
          organizationType: 'society',
        );
        orgs.select(org('owner'));
        final row = {
          ...managedCoveredActivity(_org),
          'title': 'PRIVATE-old-activity',
        };
        final pending = Completer<http.StreamedResponse>();
        if (scenario == 'lateActivityListABA') {
          client.activityRead = pending;
        } else {
          client.activities = [row];
        }
        addTearDown(() {
          orgs.dispose();
          api.dispose();
          city.dispose();
          auth.dispose();
          client.close();
        });
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: OrganizationConsolePage(
                auth: auth,
                city: city,
                organizations: orgs,
                api: api,
              ),
            ),
          ),
        );
        await _settle(t);
        VoidCallback? oldConfirm;
        if (scenario == 'oldPublishDialogABA') {
          await _tap(t, '发布');
          final button = find.widgetWithText(FilledButton, '确认发布');
          expect(
            find.descendant(
              of: find.byType(AlertDialog),
              matching: find.textContaining('PRIVATE-old-activity'),
            ),
            findsOneWidget,
          );
          oldConfirm = t.widget<FilledButton>(button).onPressed!;
        }
        orgs.select(org('member'));
        orgs.select(org('owner'));
        if (scenario == 'lateActivityListABA') {
          pending.complete(client.response([row]));
        } else {
          oldConfirm!();
        }
        await _settle(t);
        expect(client.requests.where((r) => r.method != 'GET'), isEmpty);
        if (scenario == 'lateActivityListABA') {
          expect(find.textContaining('PRIVATE-old-activity'), findsNothing);
        } else {
          expect(
            find.descendant(
              of: find.byType(AlertDialog),
              matching: find.textContaining('PRIVATE-old-activity'),
            ),
            findsNothing,
          );
          expect(find.text('请重新打开操作'), findsOneWidget);
        }
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        await _settle(t);
        expect(client.closed, isFalse);
      },
    );
  }
  for (final scenario in ['roleABA', 'ordinaryRenewal']) {
    testWidgets(
      'secondary actual organization FAQ $scenario keeps original target and retires old dialog',
      (t) async {
        _normal(t);
        final auth = SeedTestAuth()
          ..owner = _owner
          ..token = 'Bearer synthetic-A';
        final city = NowFixtureCity(), client = _Wire();
        final api = OrganizationActivityApi(
          client: client,
          apiBaseUrl: _base,
          authorizationHeader: () => auth.authorizationHeader,
        );
        final orgs = OrganizationWorkspaceController(
          client: client,
          apiBaseUrl: _base,
          authorizationHeader: () => auth.authorizationHeader,
        );
        OrganizationWorkspace org(String role) => OrganizationWorkspace(
          id: _org,
          name: '隔离合成组织',
          role: role,
          organizationType: 'society',
        );
        orgs.select(org('owner'));
        addTearDown(() {
          orgs.dispose();
          api.dispose();
          city.dispose();
          auth.dispose();
          client.close();
        });
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: OrganizationConsolePage(
                auth: auth,
                city: city,
                organizations: orgs,
                api: api,
              ),
            ),
          ),
        );
        await _settle(t);
        await _tap(t, '管理组织常见问答');
        expect(find.byType(OrganizationFAQPage), findsOneWidget);
        expect(
          client.requests.any(
            (r) => r.path == '/v1/me/organizations/$_org/faq-control',
          ),
          isTrue,
        );
        await _tap(t, '新增问答');
        await t.enterText(find.byType(TextField).at(0), '原组织问题');
        await t.enterText(find.byType(TextField).at(1), '原组织批准前的合成回答');
        final confirm = t
            .widget<FilledButton>(find.byKey(const Key('faq-check')))
            .onPressed!;
        if (scenario == 'roleABA') {
          orgs.select(org('member'));
          orgs.select(org('owner'));
          confirm();
          await _settle(t);
          expect(find.byType(AlertDialog), findsNothing);
          expect(find.textContaining('原组织批准前'), findsNothing);
          expect(client.requests.where((r) => r.method != 'GET'), isEmpty);
          expect(find.text('工作身份或来源已变化，请返回当前入口重新核实。'), findsOneWidget);
        } else {
          auth.changeIdentity('Bearer synthetic-renewed', nextOwner: _owner);
          await t.pump();
          confirm();
          await _settle(t);
          expect(client.requests.where((r) => r.method != 'GET'), isEmpty);
          await _tap(t, '重新读取当前资料');
          expect(
            t
                .widget<TextField>(find.byKey(const Key('faq-answer')))
                .controller!
                .text,
            '原组织批准前的合成回答',
          );
          await _tap(t, '检查保存问答');
          final approve = t
              .widget<FilledButton>(
                find.byKey(const Key('human-control-confirm')),
              )
              .onPressed!;
          auth.changeIdentity('Bearer synthetic-again', nextOwner: _owner);
          await _settle(t);
          expect(
            find.descendant(
              of: find.byType(AlertDialog),
              matching: find.text('原组织批准前的合成回答'),
            ),
            findsNothing,
          );
          approve();
          await _settle(t);
          expect(client.requests.where((r) => r.method == 'PUT'), isEmpty);
          expect(
            client.requests.where((r) => r.method == 'POST').single.path,
            '/v1/me/organizations/$_org/faq-control/previews',
          );
          expect(
            client.requests.where((r) => r.method == 'POST').single.token,
            'Bearer synthetic-renewed',
          );
        }
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        await _settle(t);
        expect(client.closed, isFalse);
      },
    );
  }
  for (final scenario in ['lateHistory', 'lateReceipt']) {
    testWidgets(
      'secondary actual Settings support $scenario actor ABA drops private late result',
      (t) async {
        _normal(t);
        final auth = SeedTestAuth()
          ..owner = _owner
          ..token = 'Bearer synthetic-A';
        final client = _Wire(), city = NowFixtureCity();
        final moments = PrivateMomentController(
          client: client,
          apiBaseUrl: _base,
          authorizationHeader: () => auth.authorizationHeader,
        );
        if (scenario == 'lateHistory') client.reportRead = Completer();
        addTearDown(() {
          moments.dispose();
          city.dispose();
          auth.dispose();
          client.close();
        });
        await t.pumpWidget(
          MaterialApp(
            home: Scaffold(
              body: SettingsPage(
                auth: auth,
                city: city,
                moments: moments,
                client: client,
                apiBaseUrl: _base,
              ),
            ),
          ),
        );
        await _settle(t);
        await _tap(t, '帮助与举报');
        expect(find.byType(SupportPage), findsOneWidget);
        expect(
          client.requests.where((r) => r.path == '/v1/me/reports').single.token,
          'Bearer synthetic-A',
        );
        if (scenario == 'lateReceipt') {
          await t.enterText(find.byType(TextField), '本人尚未核实的合成问题文字');
          client.reportWrite = Completer();
          await _tap(t, '检查提交问题');
          await _tap(t, '确认提交');
          expect(client.requests.where((r) => r.method == 'PUT'), hasLength(1));
        }
        auth.changeIdentity(
          'Bearer synthetic-B',
          nextOwner: '93333333-3333-4333-8333-333333333333',
        );
        auth.changeIdentity('Bearer synthetic-A', nextOwner: _owner);
        if (scenario == 'lateHistory') {
          client.reportRead!.complete(
            client.response([
              {
                'id': 'PRIVATE-old-report',
                'reason': 'safety',
                'status': 'open',
              },
            ]),
          );
        } else {
          client.reportWrite!.complete(
            client.response({
              ...humanReceiptWire(client.reportInput!),
              'resultId': 'PRIVATE-old-report',
            }),
          );
        }
        await _settle(t);
        expect(find.byType(SupportPage), findsNothing);
        expect(find.textContaining('PRIVATE-old-report'), findsNothing);
        expect(find.textContaining('本人尚未核实'), findsNothing);
        expect(
          client.requests.where(
            (r) => r.path == '/v1/me/reports' && r.method == 'GET',
          ),
          hasLength(1),
        );
        expect(
          client.requests.where((r) => r.method == 'PUT'),
          hasLength(scenario == 'lateReceipt' ? 1 : 0),
        );
        expect(
          client.requests.where((r) => r.method == 'POST'),
          hasLength(scenario == 'lateReceipt' ? 1 : 0),
        );
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        await _settle(t);
        expect(client.closed, isFalse);
      },
    );
  }
}
