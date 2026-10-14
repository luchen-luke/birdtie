import 'dart:convert';

import 'package:birdtie_client/src/workspace/task_context_privacy_api.dart';
import 'package:birdtie_client/src/workspace/task_context_privacy_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'agent_seed_sheet_test.dart' show SeedTestAuth;
import 'task_context_privacy_api_test.dart';

const _selectedID = '81000000-0000-4000-8000-000000000005';

class _TaskPageFixture {
  _TaskPageFixture({this.unknown = false, this.multiple = false}) {
    wire = TaskPrivacyWire((request) async {
      expect(request.headers['Authorization'], auth.authorizationHeader);
      if (request.method == 'GET') {
        expect((request as http.Request).bodyBytes, isEmpty);
        if (request.url.path == taskContextPrivacyPath) {
          return taskResponse(
            taskInventoryWire(
              at,
              grants: [if (multiple) taskGrantWire(at), grant],
            ),
          );
        }
        expect(request.url.path, '$taskContextPrivacyPath/$_selectedID');
        return taskResponse(taskOriginalWire(grant));
      }
      expect(request.method, 'DELETE');
      expect(request.url.path, '$taskContextPrivacyPath/$_selectedID');
      expect(jsonDecode((request as http.Request).body), {
        'expectedRevision': 4,
      });
      if (unknown) throw http.ClientException('SYNTHETIC_LOST_RESULT');
      revoked = true;
      return taskResponse(taskOriginalWire(grant));
    });
  }
  final at = DateTime.now().toUtc();
  final bool unknown, multiple;
  final auth = SeedTestAuth()..owner = taskOwner;
  final workspace = ValueNotifier<String?>(null);
  late final TaskPrivacyWire wire;
  bool revoked = false;
  Map<String, dynamic> get grant => {
    ...taskGrantWire(at, revision: revoked ? 5 : 4, revoked: revoked),
    'id': _selectedID,
    'profileFields': ['preferredActivityTypes'],
  };
  String? workspaceID() => workspace.value;
  Widget page() => MaterialApp(
    home: TaskContextPrivacyPage(
      auth: auth,
      client: wire,
      apiBaseUrl: 'https://task-page.test',
      workspaceChanges: workspace,
      organizationWorkspaceID: workspaceID,
    ),
  );
  Iterable<http.BaseRequest> get mutations =>
      wire.sent.where((r) => r.method != 'GET');
  Future<void> close(WidgetTester t) async {
    await t.pumpWidget(const SizedBox());
    await t.pumpAndSettle();
    auth.dispose();
    workspace.dispose();
    wire.close();
  }
}

Future<void> _visible(
  WidgetTester t,
  Finder target, {
  double delta = 180,
}) async {
  if (target.evaluate().isEmpty) {
    await t.scrollUntilVisible(
      target,
      delta,
      scrollable: find.byType(Scrollable).last,
      maxScrolls: 30,
    );
  }
  await Scrollable.ensureVisible(t.element(target), alignment: .3);
  await t.pumpAndSettle();
  expect(target.hitTestable(), findsOneWidget);
}

Future<void> _checkSelected(WidgetTester t, {bool returning = false}) async {
  await _visible(t, find.text('资料范围：偏好的活动类型'), delta: returning ? -180 : 180);
  final check = find.widgetWithText(OutlinedButton, '检查这项撤回').last;
  await _visible(t, check);
  await t.tap(check);
  await t.pumpAndSettle();
  await _visible(t, find.text('撤回这项资料授权'));
  expect(find.text('资料范围：偏好的活动类型').last.hitTestable(), findsOneWidget);
  expect(find.textContaining('允许时间：').last.hitTestable(), findsOneWidget);
  expect(find.textContaining('许可到期：').last.hitTestable(), findsOneWidget);
  expect(find.text('只撤回上述具体范围的读取许可，不会删除原资料、任务或记忆。'), findsOneWidget);
}

void main() {
  testWidgets('任务资料授权确认区只核对选中范围，取消零写，再确认原revision一次DELETE', (t) async {
    final s = _TaskPageFixture(multiple: true);
    try {
      await t.pumpWidget(s.page());
      await t.pumpAndSettle();
      expect(find.widgetWithText(AppBar, '任务资料授权'), findsOneWidget);
      expect(s.mutations, isEmpty);
      await _checkSelected(t, returning: true);
      await _visible(t, find.text('取消本次检查'));
      await t.tap(find.text('取消本次检查'));
      await t.pumpAndSettle();
      expect(s.mutations, isEmpty);
      await _checkSelected(t);
      await _visible(t, find.text('确认撤回这项许可'));
      await t.tap(find.text('确认撤回这项许可'));
      await t.pumpAndSettle();
      expect(s.mutations.length, 1);
      expect(
        s.wire.sent.where((r) => r.method == 'POST' || r.method == 'PUT'),
        isEmpty,
      );
      expect(s.revoked, true);
      expect(find.textContaining('已收到撤回结果'), findsOneWidget);
      expect(t.takeException(), isNull);
    } finally {
      await s.close(t);
    }
  });

  testWidgets('任务资料授权UNKNOWN仅GET原许可，不以当前状态伪称原请求回执或重发DELETE', (t) async {
    final s = _TaskPageFixture(unknown: true);
    try {
      await t.pumpWidget(s.page());
      await t.pumpAndSettle();
      await _checkSelected(t);
      await _visible(t, find.text('确认撤回这项许可'));
      await t.tap(find.text('确认撤回这项许可'));
      await t.pumpAndSettle();
      expect(s.mutations.length, 1);
      await _visible(t, find.text('仅核实原许可当前状态'));
      await t.tap(find.text('仅核实原许可当前状态'));
      await t.pumpAndSettle();
      expect(s.mutations.length, 1);
      expect(s.wire.sent.last.method, 'GET');
      expect(s.wire.sent.last.url.path, '$taskContextPrivacyPath/$_selectedID');
      expect(find.textContaining('先前提交结果仍未确认，不会重复提交'), findsOneWidget);
      expect(find.textContaining('已收到撤回结果'), findsNothing);
      expect(t.takeException(), isNull);
    } finally {
      await s.close(t);
    }
  });

  testWidgets('任务资料授权账号ABA退休旧检查，零旧DELETE', (t) async {
    final s = _TaskPageFixture();
    try {
      await t.pumpWidget(s.page());
      await t.pumpAndSettle();
      await _checkSelected(t);
      final token = s.auth.token;
      s.auth.changeIdentity('Bearer synthetic-other', nextOwner: taskAgent);
      s.auth.changeIdentity(token, nextOwner: taskOwner);
      await t.pumpAndSettle();
      expect(find.text('确认撤回这项许可'), findsNothing);
      expect(s.mutations, isEmpty);
    } finally {
      await s.close(t);
    }
  });

  testWidgets('任务资料授权匿名与组织身份均不读取本人许可', (t) async {
    for (final organization in [false, true]) {
      final s = _TaskPageFixture();
      try {
        if (organization) {
          s.workspace.value = taskAgent;
        } else {
          s.auth.changeIdentity(null, nextOwner: taskOwner);
        }
        await t.pumpWidget(s.page());
        await t.pumpAndSettle();
        expect(s.wire.sent, isEmpty);
        expect(find.text('确认撤回这项许可'), findsNothing);
      } finally {
        await s.close(t);
      }
    }
  });
}
