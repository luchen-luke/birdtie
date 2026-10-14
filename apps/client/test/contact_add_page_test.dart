import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/auth/birdtie_auth_controller.dart';
import 'package:birdtie_client/src/workspace/contact_add_page.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:qr_flutter/qr_flutter.dart';

const _self = '11111111-1111-4111-8111-111111111111';
const _other = '22222222-2222-4222-8222-222222222222';
const _third = '33333333-3333-4333-8333-333333333333';

class _Auth extends BirdtieAuthController {
  String? token = 'Bearer widget-only', id = _self, name = '我的名字';
  @override
  String? get authorizationHeader => token;
  @override
  String? get accountID => id;
  @override
  String? get displayName => name;
  @override
  bool get signedIn => token != null;
  void change(String? next) {
    id = next;
    token = next == null ? null : 'Bearer widget-$next';
    name = next == _third ? '第二账号' : null;
    notifyListeners();
  }
}

class _Scanner extends MobileScannerController {
  _Scanner() : super(autoStart: false);
  int starts = 0, stops = 0;
  MobileScannerException? nextError;
  final calls = <String>[];
  @override
  Future<void> start({
    CameraFacing? cameraDirection,
    CameraLensType? cameraLensType,
  }) async {
    starts++;
    calls.add('start');
    if (nextError != null) {
      value = value.copyWith(error: nextError, isInitialized: true);
      return;
    }
    value = value.copyWith(
      isInitialized: true,
      isRunning: true,
      size: const Size(400, 400),
    );
  }

  @override
  Future<void> stop() async {
    stops++;
    calls.add('stop');
    value = value.copyWith(isRunning: false);
  }

  @override
  Widget buildCameraView() => const ColoredBox(color: Colors.black);
}

http.Response _response(Object? data, [int status = 200]) => http.Response(
  jsonEncode({'data': data}),
  status,
  headers: {'content-type': 'application/json; charset=utf-8'},
);

Future<void> _search(WidgetTester tester, String text) async {
  await tester.enterText(find.byKey(const Key('contact-user-search')), text);
  await tester.testTextInput.receiveAction(TextInputAction.search);
  await tester.pump();
}

void main() {
  _qrApprovalBindingRegressions();
  _qrSourceBoundaryRegressions();
  testWidgets(
    'guest requests no directory data and resumes direct search after login',
    (t) async {
      final auth = _Auth()..change(null);
      var reads = 0, logins = 0;
      final client = MockClient((r) async {
        reads++;
        return _response([]);
      });
      await t.pumpWidget(
        MaterialApp(
          home: ContactSearchPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://contacts.test',
            onSignIn: () async {
              logins++;
              auth.change(_self);
            },
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(reads, 0);
      expect(find.text('登录，与朋友保持联系'), findsOneWidget);
      expect(find.text('重试'), findsNothing);
      await t.tap(find.text('登录'));
      await t.pumpAndSettle();
      expect(logins, 1);
      expect(find.byKey(const Key('contact-user-search')), findsOneWidget);
      expect(reads, 0);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    },
  );

  testWidgets(
    'direct read-only search decodes names and opening profile never writes a relation',
    (t) async {
      final auth = _Auth(), requests = <http.Request>[];
      final client = MockClient((r) async {
        requests.add(r);
        return _response([
          {'accountId': _other, 'displayName': '王同学很长的名字'},
        ]);
      });
      String? opened;
      await t.pumpWidget(
        MaterialApp(
          home: ContactSearchPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://contacts.test',
            onOpenPerson: (id) async {
              opened = id;
            },
          ),
        ),
      );
      await _search(t, '王同学');
      await t.pumpAndSettle();
      expect(requests.single.method, 'GET');
      expect(requests.single.url.path, '/v1/people/search');
      expect(requests.single.url.queryParameters, {'q': '王同学'});
      expect(requests.single.headers['Authorization'], auth.token);
      await t.tap(find.text('王同学很长的名字'));
      await t.pumpAndSettle();
      expect(opened, _other);
      expect(requests.length, 1);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    },
  );

  testWidgets(
    'logout and A-B-A account switching retire late directory responses',
    (t) async {
      final auth = _Auth(), pending = Completer<http.Response>();
      final client = MockClient((_) => pending.future);
      await t.pumpWidget(
        MaterialApp(
          home: ContactSearchPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://contacts.test',
          ),
        ),
      );
      await _search(t, '旧账号获准姓名');
      auth.change(_third);
      await t.pump();
      auth.change(_self);
      await t.pump();
      pending.complete(
        _response([
          {'accountId': _other, 'displayName': '旧账号私密可见内容'},
        ]),
      );
      await t.pumpAndSettle();
      expect(find.text('旧账号私密可见内容'), findsNothing);
      expect(
        t.widget<TextField>(find.byType(TextField)).controller!.text,
        isEmpty,
      );
      auth.change(null);
      await t.pumpAndSettle();
      expect(find.text('旧账号私密可见内容'), findsNothing);
      expect(find.byType(TextField), findsNothing);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    },
  );

  for (final status in [401, 403, 503]) {
    testWidgets(
      'HTTP $status keeps auth expiry, permission and service recovery distinct',
      (t) async {
        final auth = _Auth();
        var rejected = 0;
        final client = MockClient((_) async => _response([], status));
        await t.pumpWidget(
          MaterialApp(
            home: ContactSearchPage(
              auth: auth,
              client: client,
              apiBaseUrl: 'http://contacts.test',
              onUnauthorized: (token) async {
                rejected++;
              },
            ),
          ),
        );
        await _search(t, '王');
        await t.pumpAndSettle();
        expect(rejected, status == 401 ? 1 : 0);
        expect(find.text('重试'), status == 503 ? findsOneWidget : findsNothing);
        expect(
          find.text(switch (status) {
            401 => '登录已过期，请重新登录。',
            403 => '当前身份没有查看权限。',
            _ => '搜索服务暂不可用，请稍后重试。',
          }),
          findsOneWidget,
        );
        await t.pumpWidget(const SizedBox());
        auth.dispose();
        client.close();
      },
    );
  }

  testWidgets(
    'real empty data gives one useful action and malformed result cannot create a link',
    (t) async {
      final auth = _Auth();
      var malformed = false;
      final client = MockClient(
        (_) async => _response(
          malformed
              ? [
                  {'accountId': 'https://evil.example', 'displayName': '伪造入口'},
                ]
              : [],
        ),
      );
      await t.pumpWidget(
        MaterialApp(
          home: ContactSearchPage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://contacts.test',
          ),
        ),
      );
      await _search(t, '王');
      await t.pumpAndSettle();
      expect(find.text('没有找到可查看的用户。'), findsOneWidget);
      expect(find.text('换个姓名搜索'), findsOneWidget);
      malformed = true;
      await _search(t, '李');
      await t.pumpAndSettle();
      expect(find.text('伪造入口'), findsNothing);
      expect(find.text('搜索结果暂不可读取，请重试。'), findsOneWidget);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
      client.close();
    },
  );

  testWidgets(
    'scanner starts on its route, rejects external URL and only opens recognized profile',
    (t) async {
      final auth = _Auth(), scanner = _Scanner();
      final opened = <String>[];
      await t.pumpWidget(
        MaterialApp(
          home: ContactScanPage(
            auth: auth,
            scannerController: scanner,
            client: MockClient((r) async => _response({'accountId': _other})),
            apiBaseUrl: 'http://qr-widget.test',
            onOpenPerson: (id) async {
              scanner.calls.add('profile');
              opened.add(id);
            },
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(scanner.starts, 1);
      t.widget<MobileScanner>(find.byType(MobileScanner)).onDetect!(
        const BarcodeCapture(
          barcodes: [Barcode(rawValue: 'https://evil.example')],
        ),
      );
      await t.pumpAndSettle();
      expect(opened, isEmpty);
      expect(find.text('这不是 Birdtie 联系人二维码，请重新扫描。'), findsOneWidget);
      await t.tap(find.text('重新扫描'));
      await t.pumpAndSettle();
      t.widget<MobileScanner>(find.byType(MobileScanner)).onDetect!(
        const BarcodeCapture(
          barcodes: [
            Barcode(
              rawValue:
                  'birdtie://contact/22222222-2222-4222-8222-222222222222',
            ),
          ],
        ),
      );
      await t.pumpAndSettle();
      expect(opened, [_other]);
      expect(
        scanner.starts,
        2,
        reason: 'Back must not reopen the same QR profile automatically',
      );
      expect(
        scanner.calls.indexOf('profile'),
        greaterThan(scanner.calls.indexOf('stop')),
      );
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );

  testWidgets(
    'guest scanner never requests camera and account changes stop active scanner',
    (t) async {
      final auth = _Auth()..change(null), scanner = _Scanner();
      await t.pumpWidget(
        MaterialApp(
          home: ContactScanPage(auth: auth, scannerController: scanner),
        ),
      );
      await t.pumpAndSettle();
      expect(scanner.starts, 0);
      expect(find.byType(MobileScanner), findsNothing);
      auth.change(_self);
      await t.pumpAndSettle();
      expect(scanner.starts, 0);
      await t.tap(find.text('重新扫描'));
      await t.pumpAndSettle();
      expect(scanner.starts, 1);
      auth.change(null);
      await t.pumpAndSettle();
      expect(scanner.stops, greaterThan(0));
      expect(find.byType(MobileScanner), findsNothing);
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );

  testWidgets('camera denial gives recovery without relationship actions', (
    t,
  ) async {
    final auth = _Auth(),
        scanner = _Scanner()
          ..nextError = const MobileScannerException(
            errorCode: MobileScannerErrorCode.permissionDenied,
          );
    await t.pumpWidget(
      MaterialApp(
        home: ContactScanPage(auth: auth, scannerController: scanner),
      ),
    );
    await t.pumpAndSettle();
    expect(find.text('相机权限未开启，可在系统设置中开启，或搜索用户。'), findsOneWidget);
    expect(find.text('搜索用户'), findsOneWidget);
    expect(find.text('重新扫描'), findsOneWidget);
    expect(find.text('申请连接'), findsNothing);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
  });

  testWidgets(
    'own code replaces identity immediately and carries no name or credential',
    (t) async {
      void expectNoAccountIdentifiers() {
        expect(find.textContaining('用户标识'), findsNothing);
        expect(find.text(_self), findsNothing);
        expect(find.text(_third), findsNothing);
      }
      final auth = _Auth();
      final client = MockClient(
        (r) async => _response({
          'version': 1,
          'codeId': auth.id == _self ? _other : _self,
          'expiresAt': DateTime.utc(2099).toIso8601String(),
        }),
      );
      await t.pumpWidget(
        MaterialApp(
          home: MyContactCodePage(
            auth: auth,
            client: client,
            apiBaseUrl: 'http://qr-widget.test',
          ),
        ),
      );
      await t.pumpAndSettle();
      expect(
        t.widget<QrImageView>(find.byType(QrImageView)).key,
        const ValueKey('contact-code:$_other'),
      );
      expect(find.text('我的名字'), findsOneWidget);
      expectNoAccountIdentifiers();
      auth.change(_third);
      await t.pumpAndSettle();
      expect(find.text('我的名字'), findsNothing);
      expect(find.text('第二账号'), findsOneWidget);
      expect(
        t.widget<QrImageView>(find.byType(QrImageView)).key,
        const ValueKey('contact-code:$_self'),
      );
      expectNoAccountIdentifiers();
      auth.change(null);
      await t.pumpAndSettle();
      expect(find.byType(QrImageView), findsNothing);
      expectNoAccountIdentifiers();
      await t.pumpWidget(const SizedBox());
      auth.dispose();
    },
  );

  testWidgets('long name at large text size remains scrollable and readable', (
    t,
  ) async {
    final auth = _Auth();
    final client = MockClient(
      (_) async => _response([
        {'accountId': _other, 'displayName': '这是需要在资料中完整显示的非常长的联系人姓名' * 3},
      ]),
    );
    await t.pumpWidget(
      MaterialApp(
        builder: (_, child) => MediaQuery(
          data: const MediaQueryData(
            size: Size(320, 640),
            textScaler: TextScaler.linear(2.0),
          ),
          child: child!,
        ),
        home: ContactSearchPage(
          auth: auth,
          client: client,
          apiBaseUrl: 'http://contacts.test',
        ),
      ),
    );
    await _search(t, '姓名');
    await t.pumpAndSettle();
    expect(t.takeException(), isNull);
    expect(find.byType(ListTile), findsOneWidget);
    await t.pumpWidget(const SizedBox());
    auth.dispose();
    client.close();
  });
}

// Focused actual-widget route binding regressions; scanner hardware is replaced
// only at its existing public controller/onDetect seam. No native QA claim.
void _qrApprovalBindingRegressions() {
  for (final scenario in ['normal', 'same-source-route-rebuild', 'owner-ABA']) {
    testWidgets('QR full profile approval $scenario', (t) async {
      final auth = _Auth()..token = 'Bearer widget-$_self';
      final scanner = _Scanner();
      final permission = Completer<http.Response>();
      var permissionReads = 0, profileReads = 0, writes = 0;
      final requests = <http.Request>[];
      Map<String, dynamic> actions() {
        final now = DateTime.now().toUtc();
        return {
          'schema': 'entity-actions-v1',
          'entityRef': {'type': 'person', 'id': _other},
          'title': '合成扫码对象',
          'sourceVersion': 'a' * 64,
          'observedAt': now.toIso8601String(),
          'validUntil': now.add(const Duration(seconds: 25)).toIso8601String(),
          'actions': [
            for (final entry in <String, String>{
              'CONNECT': 'REQUEST_FRIEND',
              'MESSAGE': 'OPEN_CHAT',
              'SHARE': 'CHOOSE_RECIPIENT',
              'JOIN': 'JOIN',
              'SAVE': 'SAVE',
              'NAVIGATE': 'NAVIGATE',
            }.entries)
              {
                'kind': entry.key,
                'operation': entry.value,
                'targetRef': {'type': 'person', 'id': _other},
                'state': entry.key == 'CONNECT' ? 'AVAILABLE' : 'UNAVAILABLE',
                'label': '具体操作',
                'reason': '提交前重新核验',
                'requiresConfirmation': entry.key != 'MESSAGE',
              },
          ],
        };
      }

      final client = MockClient((request) async {
        requests.add(request);
        if (request.url.path == '/v1/contact-code/resolve')
          return _response({'accountId': _other});
        if (request.method == 'POST') {
          writes++;
          expect(request.url.path, '/v1/me/connection-requests');
          expect(jsonDecode(request.body), {
            'recipientAccountId': _other,
            'scope': 'friend',
            'note': '合成扫码申请说明',
          });
          return _response({}, 201);
        }
        if (request.url.path == '/v1/me/entity-actions/person/$_other') {
          permissionReads++;
          return permissionReads == 1
              ? permission.future
              : _response(actions());
        }
        if (request.url.path == '/v1/accounts/$_other/profile') {
          profileReads++;
          return _response({
            'accountId': _other,
            'displayName': '合成扫码对象',
            'visibility': 'public',
          });
        }
        if (request.url.path == '/v1/me/follows') return _response([]);
        return _response({}, 404);
      });
      await t.pumpWidget(
        MaterialApp(
          home: ContactScanPage(
            auth: auth,
            scannerController: scanner,
            client: client,
            apiBaseUrl: 'http://qr-widget.test',
          ),
        ),
      );
      await t.pumpAndSettle();
      t.widget<MobileScanner>(find.byType(MobileScanner)).onDetect!(
        const BarcodeCapture(
          barcodes: [
            Barcode(
              rawValue:
                  'birdtie://contact/22222222-2222-4222-8222-222222222222',
            ),
          ],
        ),
      );
      await t.pumpAndSettle();
      expect(find.text('合成扫码对象'), findsOneWidget);
      expect(writes, 0, reason: 'QR recognition only opens the actual profile');
      final profileRoute = ModalRoute.of(t.element(find.text('添加好友')))!;
      await t.tap(find.text('添加好友'));
      await t.pumpAndSettle();
      await t.enterText(find.byType(TextField), '合成扫码申请说明');
      await t.pump();
      await t.tap(find.text('检查申请'));
      await t.pumpAndSettle();
      expect(permissionReads, 1);
      expect(find.text('检查申请'), findsNothing);
      expect(
        writes,
        0,
        reason: 'Reading action permission never applies a request',
      );

      final readsBeforeRouteChange = profileReads;
      if (scenario == 'same-source-route-rebuild') {
        // Public Flutter route hook: same source, account, token and transport.
        // Navigator dependency/route rebuild must not invent a source rebind.
        profileRoute.changedExternalState();
        await t.pumpAndSettle();
        expect(auth.accountID, _self);
        expect(auth.authorizationHeader, 'Bearer widget-$_self');
      } else if (scenario == 'owner-ABA') {
        auth.change(_third);
        await t.pumpAndSettle();
        auth.change(_self);
        await t.pumpAndSettle();
      }
      permission.complete(_response(actions()));
      await t.pumpAndSettle();
      if (scenario == 'owner-ABA') {
        expect(find.text('发送好友申请'), findsNothing);
        expect(
          writes,
          0,
          reason: 'Real account A-B-A retires this old approval',
        );
      } else {
        expect(
          find.text('发送好友申请'),
          findsOneWidget,
          reason:
              'Same-binding route repaint must preserve the explicit final approval',
        );
        expect(
          profileReads,
          readsBeforeRouteChange,
          reason:
              'Route repaint must not discard and reload the same profile source',
        );
        expect(
          writes,
          0,
          reason: 'Final approval is required even after valid read',
        );
        await t.tap(find.text('发送好友申请'));
        await t.pumpAndSettle();
        expect(
          permissionReads,
          2,
          reason: 'Explicit confirmation rechecks real read permission',
        );
        expect(writes, 1);
      }
      expect(
        requests.where(
          (r) => r.method == 'POST' && r.url.path.contains('/messages'),
        ),
        isEmpty,
      );
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      auth.dispose();
      client.close();
    });
  }
}

class _QrSameValueAuth extends _Auth {
  void sameValueNotice() => notifyListeners();
}

void _qrSourceBoundaryRegressions() {
  _qrAuthRotationRecoveryRegression();
  for (final scenario in [
    'loaded-private-boundary',
    'pending-boundary',
    'same-auth-notify',
  ]) {
    testWidgets('QR source boundary $scenario', (t) async {
      final auth = _QrSameValueAuth()..token = 'Bearer widget-$_self';
      final boundary = ValueNotifier(0), scanner = _Scanner();
      final permission = Completer<http.Response>();
      var writes = 0, profileReads = 0, permissionReads = 0;
      Map<String, dynamic> actionData() {
        final now = DateTime.now().toUtc();
        return {
          'schema': 'entity-actions-v1',
          'entityRef': {'type': 'person', 'id': _other},
          'title': '合成来源对象',
          'sourceVersion': 'a' * 64,
          'observedAt': now.toIso8601String(),
          'validUntil': now.add(const Duration(seconds: 25)).toIso8601String(),
          'actions': [
            for (final entry in <String, String>{
              'CONNECT': 'REQUEST_FRIEND',
              'MESSAGE': 'OPEN_CHAT',
              'SHARE': 'CHOOSE_RECIPIENT',
              'JOIN': 'JOIN',
              'SAVE': 'SAVE',
              'NAVIGATE': 'NAVIGATE',
            }.entries)
              {
                'kind': entry.key,
                'operation': entry.value,
                'targetRef': {'type': 'person', 'id': _other},
                'state': entry.key == 'CONNECT' ? 'AVAILABLE' : 'UNAVAILABLE',
                'label': '具体操作',
                'reason': '提交前重新核验',
                'requiresConfirmation': entry.key != 'MESSAGE',
              },
          ],
        };
      }

      final client = MockClient((r) async {
        if (r.url.path == '/v1/contact-code/resolve')
          return _response({'accountId': _other});
        if (r.method == 'POST') {
          writes++;
          return _response({}, 201);
        }
        if (r.url.path == '/v1/accounts/$_other/profile') {
          profileReads++;
          if (scenario == 'loaded-private-boundary' &&
              r.headers['Authorization'] == null) {
            return _response({}, 404);
          }
          return _response({
            'accountId': _other,
            'displayName': '合成来源对象',
            'visibility': scenario == 'loaded-private-boundary'
                ? 'connections'
                : 'public',
            'bio': scenario == 'loaded-private-boundary' ? '旧来源可见的私密资料' : '',
          });
        }
        if (r.url.path == '/v1/me/entity-actions/person/$_other') {
          permissionReads++;
          return permission.future;
        }
        if (r.url.path == '/v1/me/follows') return _response([]);
        return _response({}, 404);
      });
      await t.pumpWidget(
        MaterialApp(
          home: ContactScanPage(
            auth: auth,
            identityChanges: boundary,
            scannerController: scanner,
            client: client,
            apiBaseUrl: 'http://qr-source.test',
          ),
        ),
      );
      await t.pumpAndSettle();
      t.widget<MobileScanner>(find.byType(MobileScanner)).onDetect!(
        const BarcodeCapture(
          barcodes: [
            Barcode(
              rawValue:
                  'birdtie://contact/22222222-2222-4222-8222-222222222222',
            ),
          ],
        ),
      );
      await t.pumpAndSettle();
      expect(profileReads, 1);
      final profileNavigator = Navigator.of(t.element(find.text('合成来源对象')));
      if (scenario == 'loaded-private-boundary') {
        expect(find.text('旧来源可见的私密资料'), findsOneWidget);
        boundary.value++;
        await t.pumpAndSettle();
        expect(auth.accountID, _self);
        expect(auth.authorizationHeader, 'Bearer widget-$_self');
        expect(
          find.text('旧来源可见的私密资料'),
          findsNothing,
          reason:
              'A same-identity provider boundary retires old private content immediately',
        );
      } else {
        await t.tap(find.text('添加好友'));
        await t.pumpAndSettle();
        await t.enterText(find.byType(TextField), '合成来源申请说明');
        await t.pump();
        await t.tap(find.text('检查申请'));
        await t.pumpAndSettle();
        expect(permissionReads, 1);
        if (scenario == 'same-auth-notify') {
          auth.sameValueNotice();
        } else {
          boundary.value++;
        }
        await t.pumpAndSettle();
        permission.complete(_response(actionData()));
        await t.pumpAndSettle();
        expect(auth.accountID, _self);
        expect(auth.authorizationHeader, 'Bearer widget-$_self');
        if (scenario == 'same-auth-notify') {
          expect(
            find.text('发送好友申请'),
            findsOneWidget,
            reason:
                'Ordinary same-value auth notification is not provider retirement',
          );
          expect(profileReads, 1);
          await t.tap(find.text('取消'));
          await t.pumpAndSettle();
        } else {
          expect(
            find.text('发送好友申请'),
            findsNothing,
            reason:
                'Late old-source permissions cannot create a current approval',
          );
        }
      }
      expect(writes, 0);
      if (scenario != 'same-auth-notify') {
        profileNavigator.pop();
        await t.pumpAndSettle();
        expect(find.text('内容已更新'), findsOneWidget);
        expect(find.text('请返回联系人后重新打开。'), findsOneWidget);
        expect(
          find.text('登录，与朋友保持联系'),
          findsNothing,
          reason:
              'Same-account source retirement is not authentication failure',
        );
      }
      await t.pumpWidget(const SizedBox());
      await t.pumpAndSettle();
      boundary.dispose();
      auth.dispose();
      client.close();
    });
  }
}

void _qrAuthRotationRecoveryRegression() {
  testWidgets(
    'QR source boundary same-owner token rotation preserves search draft',
    (t) async {
      final auth = _QrSameValueAuth()..token = 'Bearer widget-$_self';
      final boundary = ValueNotifier(0);
      var observed = (auth.authorizationHeader, auth.accountID);
      void mirrorRealIdentityChange() {
        final next = (auth.authorizationHeader, auth.accountID);
        if (next != observed) {
          observed = next;
          boundary.value++;
        }
      }

      // Host listens before the opened route, as MapWorkspace does.
      auth.addListener(mirrorRealIdentityChange);
      var directoryReads = 0, writes = 0;
      final client = MockClient((r) async {
        if (r.method != 'GET') writes++;
        directoryReads++;
        return _response([
          {'accountId': _other, 'displayName': '旧读取授权姓名'},
        ]);
      });
      await t.pumpWidget(
        MaterialApp(
          home: ContactSearchPage(
            auth: auth,
            identityChanges: boundary,
            client: client,
            apiBaseUrl: 'http://qr-auth.test',
          ),
        ),
      );
      await _search(t, '合成同账号搜索草稿');
      await t.pumpAndSettle();
      expect(directoryReads, 1);
      expect(find.text('旧读取授权姓名'), findsOneWidget);
      auth.token = 'Bearer rotated-same-owner';
      auth.sameValueNotice();
      await t.pumpAndSettle();
      expect(find.text('旧读取授权姓名'), findsNothing);
      expect(
        find.text('内容已更新'),
        findsNothing,
        reason:
            'A real token change is handled by the identity path, not provider retirement',
      );
      expect(
        t
            .widget<TextField>(find.byKey(const Key('contact-user-search')))
            .controller!
            .text,
        '合成同账号搜索草稿',
      );
      expect(
        directoryReads,
        1,
        reason:
            'Identity recovery does not automatically repeat a private read',
      );
      expect(writes, 0);
      await _search(t, '合成同账号搜索草稿');
      await t.pumpAndSettle();
      expect(
        directoryReads,
        2,
        reason: 'An explicit new search may read with current identity',
      );
      await t.pumpWidget(const SizedBox());
      auth.removeListener(mirrorRealIdentityChange);
      boundary.dispose();
      auth.dispose();
      client.close();
    },
  );
}
