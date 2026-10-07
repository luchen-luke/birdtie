import 'dart:async';
import 'package:birdtie_client/src/workspace/agent_request_failure.dart';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/agent_task_detail_page.dart';
import 'package:birdtie_client/src/workspace/remote_agent_task_source.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const taskDestinationID = '22222222-2222-4222-8222-222222222222';
const taskDestinationOwner = '11111111-1111-4111-8111-111111111111';
Map<String, dynamic> taskDestinationData({
  String id = taskDestinationID,
  String owner = taskDestinationOwner,
}) => {
  'taskId': id,
  'task': {
    'id': id,
    'query': '周末羽毛球',
    'status': 'COMPLETED',
    'principalType': 'person',
    'principalId': owner,
    'actingUserId': owner,
    'conversation': [
      {'role': 'assistant', 'text': '当前没有符合条件的已发布活动。'},
    ],
  },
  'activities': [],
  'people': [],
  'groups': [],
  'places': [],
  'note': '当前已发布信息',
};
http.Response taskReply(Map<String, dynamic> data) =>
    http.Response.bytes(utf8.encode(jsonEncode({'data': data})), 200);

void main() {
  for (final status in [401, 403]) {
    for (final retired in ['session', 'organization']) {
      test(
        'GET recovery notification late HTTP$status after $retired preserves actor rejection first',
        () async {
          String? token = 'Bearer synthetic-owner';
          String? workspace;
          final source = RemoteAgentTaskSource(
            cityID: () => null,
            authorizationHeader: () => token,
            organizationWorkspaceID: () => workspace,
            apiBaseUrl: 'https://get-recovery-fixture.test',
            client: MockClient((r) async {
              expect(r.method, 'GET');
              if (retired == 'session') {
                token = 'Bearer another-owner';
              } else {
                workspace = taskDestinationID;
              }
              return http.Response(
                jsonEncode({
                  'error': {
                    'code': status == 401 ? 'unauthorized' : 'forbidden',
                  },
                }),
                status,
              );
            }),
          );
          await expectLater(
            source.readByID(taskDestinationID, ownerID: taskDestinationOwner),
            throwsA(
              isA<AgentRequestFailure>()
                  .having(
                    (e) => e.userMessage,
                    'identity retirement',
                    '工作身份已变化，请重新打开通知。',
                  )
                  .having(
                    (e) => e.statusCode,
                    'not current permission failure',
                    null,
                  ),
            ),
          );
          source.dispose();
        },
      );
    }
    testWidgets(
      'GET recovery notification late HTTP$status after original destination replacement cannot pollute new task',
      (t) async {
        final held = Completer<http.Response>();
        final oldRequests = <http.Request>[], newRequests = <http.Request>[];
        final oldSource = RemoteAgentTaskSource(
          cityID: () => null,
          authorizationHeader: () => 'Bearer synthetic-owner',
          apiBaseUrl: 'https://get-recovery-fixture.test',
          client: MockClient((r) async {
            oldRequests.add(r);
            return held.future;
          }),
        );
        final newSource = RemoteAgentTaskSource(
          cityID: () => null,
          authorizationHeader: () => 'Bearer synthetic-new-owner',
          apiBaseUrl: 'https://get-recovery-fixture.test',
          client: MockClient((r) async {
            newRequests.add(r);
            return taskReply(
              taskDestinationData(
                id: taskDestinationOwner,
                owner: taskDestinationID,
              ),
            );
          }),
        );
        Widget page(bool current) => MaterialApp(
          home: AgentTaskDetailPage(
            key: const Key('same-task-page-state'),
            taskID: current ? taskDestinationOwner : taskDestinationID,
            ownerID: current ? taskDestinationID : taskDestinationOwner,
            source: current ? newSource : oldSource,
          ),
        );
        await t.pumpWidget(page(false));
        await t.pump(const Duration(milliseconds: 20));
        await t.pumpWidget(page(true));
        await t.pumpAndSettle();
        expect(find.text('周末羽毛球'), findsOneWidget);
        held.complete(
          http.Response(
            jsonEncode({
              'error': {'code': status == 401 ? 'unauthorized' : 'forbidden'},
            }),
            status,
          ),
        );
        await t.pumpAndSettle();
        expect(oldRequests.single.method, 'GET');
        expect(newRequests.single.method, 'GET');
        expect(
          newRequests.single.url.path,
          '/v1/me/agent-tasks/$taskDestinationOwner',
        );
        expect(find.text('周末羽毛球'), findsOneWidget);
        expect(find.text('重新核实'), findsNothing);
        expect(find.text('登录状态已失效，请重新登录后重试。'), findsNothing);
        expect(find.text('当前账号没有执行此操作的权限。'), findsNothing);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        oldSource.dispose();
        newSource.dispose();
      },
    );
  }

  for (final status in [401, 403, 503]) {
    test(
      'GET recovery notification HTTP$status retains status/code and exact owned read',
      () async {
        final requests = <http.Request>[];
        final source = RemoteAgentTaskSource(
          cityID: () => null,
          authorizationHeader: () => 'Bearer synthetic-owner',
          apiBaseUrl: 'https://get-recovery-fixture.test',
          client: MockClient((r) async {
            requests.add(r);
            return http.Response(
              jsonEncode({
                'error': {'code': 'fixture_$status'},
              }),
              status,
            );
          }),
        );
        Object? caught;
        try {
          await source.readByID(
            taskDestinationID,
            ownerID: taskDestinationOwner,
          );
        } catch (e) {
          caught = e;
        }
        expect(requests.single.method, 'GET');
        expect(
          requests.single.url.path,
          '/v1/me/agent-tasks/$taskDestinationID',
        );
        expect(caught, isA<AgentRequestFailure>());
        expect((caught as AgentRequestFailure).statusCode, status);
        expect(caught.code, 'fixture_$status');
        source.dispose();
      },
    );
  }
  for (final status in [401, 403]) {
    testWidgets(
      'GET recovery notification HTTP$status explains recovery and keeps human recheck GET',
      (t) async {
        final requests = <http.Request>[];
        final source = RemoteAgentTaskSource(
          cityID: () => null,
          authorizationHeader: () => 'Bearer synthetic-owner',
          apiBaseUrl: 'https://get-recovery-fixture.test',
          client: MockClient((r) async {
            requests.add(r);
            if (requests.length == 1) return taskReply(taskDestinationData());
            if (requests.length == 2) {
              return http.Response(
                jsonEncode({
                  'error': {
                    'code': status == 401 ? 'unauthorized' : 'forbidden',
                  },
                }),
                status,
              );
            }
            return taskReply(taskDestinationData());
          }),
        );
        await t.pumpWidget(
          MaterialApp(
            home: AgentTaskDetailPage(
              taskID: taskDestinationID,
              ownerID: taskDestinationOwner,
              source: source,
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(find.text('周末羽毛球'), findsOneWidget);
        await t.tap(find.text('刷新当前状态'));
        await t.pumpAndSettle();
        expect(find.text('周末羽毛球'), findsNothing);
        expect(
          find.text(status == 401 ? '登录状态已失效，请重新登录后重试。' : '当前账号没有执行此操作的权限。'),
          findsOneWidget,
        );
        expect(find.text('重新核实'), findsOneWidget);
        final recovery = find.text(
          status == 401
              ? '请先通过个人资料重新登录，再核实当前任务。'
              : '请先确认当前身份和访问权限；恢复权限后再核实当前任务。',
        );
        await t.ensureVisible(recovery);
        await t.pumpAndSettle();
        expect(recovery.hitTestable(), findsOneWidget);
        await t.ensureVisible(find.text('重新核实'));
        await t.pumpAndSettle();
        expect(find.text('重新核实').hitTestable(), findsOneWidget);
        await t.tap(find.text('重新核实'));
        await t.pumpAndSettle();
        expect(requests, hasLength(3));
        for (final r in requests) {
          expect(r.method, 'GET');
          expect(r.url.path, '/v1/me/agent-tasks/$taskDestinationID');
        }
        expect(find.text('周末羽毛球'), findsOneWidget);
        expect(recovery, findsNothing);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        source.dispose();
      },
    );
  }
  for (final failure in ['503', 'network']) {
    testWidgets(
      'GET recovery notification $failure remains explicit human read and no POST',
      (t) async {
        final requests = <http.Request>[];
        final source = RemoteAgentTaskSource(
          cityID: () => null,
          authorizationHeader: () => 'Bearer synthetic-owner',
          apiBaseUrl: 'https://get-recovery-fixture.test',
          client: MockClient((r) async {
            requests.add(r);
            if (requests.length == 1) {
              if (failure == 'network') {
                throw http.ClientException('synthetic network');
              }
              return http.Response('{}', 503);
            }
            return taskReply(taskDestinationData());
          }),
        );
        await t.pumpWidget(
          MaterialApp(
            home: AgentTaskDetailPage(
              taskID: taskDestinationID,
              ownerID: taskDestinationOwner,
              source: source,
            ),
          ),
        );
        await t.pumpAndSettle();
        expect(find.text('周末羽毛球'), findsNothing);
        expect(find.text('重新核实'), findsOneWidget);
        await t.tap(find.text('重新核实'));
        await t.pumpAndSettle();
        expect(requests, hasLength(2));
        for (final r in requests) {
          expect(r.method, 'GET');
        }
        expect(find.text('周末羽毛球'), findsOneWidget);
        expect(t.takeException(), isNull);
        await t.pumpWidget(const SizedBox());
        source.dispose();
      },
    );
  }

  for (final mode in [
    'valid',
    'foreign',
    'wrong-id',
    'late-session',
    'organization',
  ]) {
    test('notification reads only original owned task: $mode', () async {
      String? token = 'Bearer original';
      String? workspace;
      var calls = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => null,
        authorizationHeader: () => token,
        organizationWorkspaceID: () => workspace,
        apiBaseUrl: 'https://fixture',
        client: MockClient((r) async {
          calls++;
          expect(r.method, 'GET');
          expect(r.url.path, '/v1/me/agent-tasks/$taskDestinationID');
          if (mode == 'late-session') token = null;
          return taskReply(
            taskDestinationData(
              id: mode == 'wrong-id' ? taskDestinationOwner : taskDestinationID,
              owner: mode == 'foreign'
                  ? taskDestinationID
                  : taskDestinationOwner,
            ),
          );
        }),
      );
      if (mode == 'organization') workspace = taskDestinationID;
      if (mode == 'valid') {
        final result = await source.readByID(
          taskDestinationID,
          ownerID: taskDestinationOwner,
        );
        expect(result.task!.query, '周末羽毛球');
      } else {
        await expectLater(
          source.readByID(taskDestinationID, ownerID: taskDestinationOwner),
          throwsA(isA<Object>()),
        );
      }
      expect(calls, mode == 'organization' ? 0 : 1);
      source.dispose();
    });
  }
  test('malformed or anonymous task cannot make a request', () async {
    var calls = 0;
    final source = RemoteAgentTaskSource(
      cityID: () => null,
      authorizationHeader: () => null,
      apiBaseUrl: 'https://fixture',
      client: MockClient((r) async {
        calls++;
        return taskReply(taskDestinationData());
      }),
    );
    for (final id in ['local-1', taskDestinationID]) {
      await expectLater(
        source.readByID(id, ownerID: taskDestinationOwner),
        throwsA(isA<Object>()),
      );
    }
    expect(calls, 0);
    source.dispose();
  });
  testWidgets(
    'task page shows Chinese current answer; only GET refresh, large text reachable',
    (tester) async {
      tester.view.physicalSize = const Size(320, 640);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      var calls = 0;
      final source = RemoteAgentTaskSource(
        cityID: () => null,
        authorizationHeader: () => 'Bearer original',
        apiBaseUrl: 'https://fixture',
        client: MockClient((r) async {
          calls++;
          expect(r.method, 'GET');
          return taskReply(taskDestinationData());
        }),
      );
      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(2)),
            child: child!,
          ),
          home: AgentTaskDetailPage(
            taskID: taskDestinationID,
            ownerID: taskDestinationOwner,
            source: source,
          ),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('我的任务'), findsOneWidget);
      expect(find.text('当前没有符合条件的已发布活动。'), findsOneWidget);
      await tester.scrollUntilVisible(find.text('刷新当前状态'), 150);
      await tester.tap(find.text('刷新当前状态'));
      await tester.pumpAndSettle();
      expect(calls, 2);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      source.dispose();
    },
  );
  testWidgets('denied refresh clears prior task content', (tester) async {
    var deny = false;
    final source = RemoteAgentTaskSource(
      cityID: () => null,
      authorizationHeader: () => 'Bearer original',
      apiBaseUrl: 'https://fixture',
      client: MockClient(
        (r) async =>
            deny ? http.Response('{}', 401) : taskReply(taskDestinationData()),
      ),
    );
    await tester.pumpWidget(
      MaterialApp(
        home: AgentTaskDetailPage(
          taskID: taskDestinationID,
          ownerID: taskDestinationOwner,
          source: source,
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('周末羽毛球'), findsOneWidget);
    deny = true;
    await tester.tap(find.text('刷新当前状态'));
    await tester.pumpAndSettle();
    expect(find.text('周末羽毛球'), findsNothing);
    expect(find.text('重新核实'), findsOneWidget);
    await tester.pumpWidget(const SizedBox());
    source.dispose();
  });
}
