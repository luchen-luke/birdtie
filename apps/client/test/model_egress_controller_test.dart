import 'dart:async';
import 'dart:convert';
import 'package:birdtie_client/src/workspace/model_egress_api.dart';
import 'package:birdtie_client/src/workspace/model_egress_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'model_egress_api_test.dart';

void main() {
  test(
    'double approve sends the original ID once while first response is pending',
    () async {
      final response = Completer<http.Response>();
      final entered = Completer<void>();
      var posts = 0;
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          posts++;
          entered.complete();
          return response.future;
        }),
      );
      final c = ModelEgressController(
        api: api,
        authorizationHeader: () => 'own',
        accountID: () => egressOwner,
        organizationWorkspaceID: () => null,
      );
      c.synchronizeIdentity();
      c.preview = EgressPreview(
        egressPreviewWire(deadline: egressStamp()),
        egressOwner,
      );
      final first = c.approve();
      await c.approve();
      await entered.future;
      expect(posts, 1);
      response.complete(
        egressResponse({
          'previewId': egressPreviewID,
          'status': 'APPROVED',
          'evidence': 'LOCAL_SYNTHETIC',
          'modelAccess': 'UNAVAILABLE',
        }),
      );
      await first;
      expect(c.message, contains('已确认'));
      expect(c.pendingID, egressPreviewID);
      c.dispose();
    },
  );
  test(
    'unknown preview cannot be cleared by an unrelated history item',
    () async {
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          if (r.url.path.endsWith('/options')) {
            return egressResponse(egressOptionsWire());
          }
          if (r.url.path.endsWith('/budget')) {
            return egressResponse(egressBudgetsWire());
          }
          if (r.method == 'POST') {
            throw http.ClientException('lost preview response');
          }
          if (r.url.path.endsWith('/previews')) {
            return egressResponse([egressReceiptWire()]);
          }
          return egressResponse(egressReceiptWire());
        }),
      );
      final c = ModelEgressController(
        api: api,
        authorizationHeader: () => 'own',
        accountID: () => egressOwner,
        organizationWorkspaceID: () => null,
      );
      await c.load();
      await c.choose(c.options.single);
      await c.prepare();
      expect(c.unknownPreview, isTrue);
      await c.inspect(egressPreviewID);
      expect(c.unknownPreview, isTrue);
      expect(c.preview, isNull);
      expect(c.current!.revocable, isTrue);
      c.dispose();
    },
  );
  test(
    'unknown revoke keeps same ID and blocks unrelated result substitution',
    () async {
      var deletes = 0, reads = 0;
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          if (r.method == 'DELETE') {
            deletes++;
            throw http.ClientException('lost');
          }
          reads++;
          return egressResponse(egressReceiptWire(status: 'REVOKED'));
        }),
      );
      final c = ModelEgressController(
        api: api,
        authorizationHeader: () => 'own',
        accountID: () => egressOwner,
        organizationWorkspaceID: () => null,
      );
      await c.revoke(egressPreviewID);
      expect(c.unknown, isTrue);
      await c.inspect(egressTask);
      await c.revoke(egressTask);
      expect(reads, 0);
      expect(deletes, 1);
      expect(c.pendingID, egressPreviewID);
      await c.inspect(egressPreviewID);
      expect(c.unknown, isFalse);
      expect(c.current!.status, 'REVOKED');
      expect(deletes, 1);
      c.dispose();
    },
  );
  test(
    'expired concrete preview and transport failures never auto renew',
    () async {
      var posts = 0;
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          posts++;
          return egressResponse({});
        }),
      );
      final c = ModelEgressController(
        api: api,
        authorizationHeader: () => 'own',
        accountID: () => egressOwner,
        organizationWorkspaceID: () => null,
      );
      c.synchronizeIdentity();
      c.preview = EgressPreview(
        egressPreviewWire(
          deadline: egressStamp(
            DateTime.now().toUtc().subtract(const Duration(seconds: 1)),
          ),
        ),
        egressOwner,
      );
      await c.approve();
      expect(posts, 0);
      expect(c.preview, isNull);
      c.dispose();
    },
  );
  test(
    'lost approval queries original native ID and never creates or resends',
    () async {
      var previews = 0, approvals = 0;
      Map<String, dynamic>? p;
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          if (r.url.path.endsWith('/options')) {
            return egressResponse(egressOptionsWire());
          }
          if (r.url.path.endsWith('/budget')) {
            return egressResponse(egressBudgetsWire());
          }
          if (r.method == 'GET' && r.url.path.endsWith('/previews')) {
            return egressResponse([]);
          }
          if (r.method == 'POST' && r.url.path.endsWith('/previews')) {
            previews++;
            p = egressPreviewWire(
              deadline: (jsonDecode(r.body) as Map)['deadlineAt'],
            );
            return egressResponse(p);
          }
          if (r.url.path.endsWith('/approvals')) {
            approvals++;
            throw http.ClientException('response lost');
          }
          p!['status'] = 'APPROVED';
          return egressResponse(
            egressReceiptWire(status: 'APPROVED', review: p),
          );
        }),
      );
      final c = ModelEgressController(
        api: api,
        authorizationHeader: () => 'Bearer own',
        accountID: () => egressOwner,
        organizationWorkspaceID: () => null,
      );
      await c.load();
      await c.choose(c.options.single);
      await c.prepare();
      await c.approve();
      expect(c.unknown, isTrue);
      expect(c.preview, isNull);
      await c.prepare();
      await c.approve();
      expect(previews, 1);
      expect(approvals, 1);
      await c.inspect(c.pendingID!);
      expect(c.unknown, isFalse);
      expect(c.current!.status, 'APPROVED');
      await c.approve();
      expect(approvals, 1);
      c.dispose();
    },
  );
  test(
    'late A response and identity ABA cannot replace B state or send approval',
    () async {
      String? token = 'A', workspace;
      final response = Completer<http.Response>();
      var posts = 0;
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          if (r.method == 'POST') {
            posts++;
          }
          if (r.url.path.endsWith('/options')) return response.future;
          return egressResponse([]);
        }),
      );
      final c = ModelEgressController(
        api: api,
        authorizationHeader: () => token,
        accountID: () => egressOwner,
        organizationWorkspaceID: () => workspace,
      );
      final loading = c.load();
      token = 'B';
      c.synchronizeIdentity();
      token = 'A';
      c.synchronizeIdentity();
      response.complete(egressResponse(egressOptionsWire()));
      await loading;
      expect(c.options, isEmpty);
      await c.approve();
      expect(posts, 0);
      workspace = egressRoot;
      c.synchronizeIdentity();
      expect(c.personal, isFalse);
      expect(c.preview, isNull);
      c.dispose();
    },
  );
  test(
    'original-session-only metadata cannot be approved after restart',
    () async {
      var approvals = 0;
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          if (r.method == 'POST') approvals++;
          if (r.url.path.endsWith('/options')) {
            return egressResponse(egressOptionsWire([]));
          }
          if (r.url.path.endsWith('/previews')) {
            return egressResponse([egressReceiptWire(status: 'APPROVED')]);
          }
          return egressResponse(egressReceiptWire(status: 'APPROVED'));
        }),
      );
      final c = ModelEgressController(
        api: api,
        authorizationHeader: () => 'new Session',
        accountID: () => egressOwner,
        organizationWorkspaceID: () => null,
      );
      await c.load();
      await c.inspect(c.receipts.single.id);
      expect(c.preview, isNull);
      expect(c.current!.revocable, isTrue);
      await c.approve();
      expect(approvals, 0);
      c.dispose();
    },
  );
  test(
    'cancel review keeps original ID and disposal rejects late prepare',
    () async {
      final delayed = Completer<http.Response>();
      var posts = 0;
      final api = ModelEgressAPI(
        apiBaseUrl: 'http://fixture',
        client: MockClient((r) async {
          if (r.url.path.endsWith('/options')) {
            return egressResponse(egressOptionsWire());
          }
          if (r.url.path.endsWith('/budget')) {
            return egressResponse(egressBudgetsWire());
          }
          if (r.method == 'POST') {
            posts++;
            return delayed.future;
          }
          return egressResponse([]);
        }),
      );
      final c = ModelEgressController(
        api: api,
        authorizationHeader: () => 'own',
        accountID: () => egressOwner,
        organizationWorkspaceID: () => null,
      );
      await c.load();
      await c.choose(c.options.single);
      final preparing = c.prepare();
      c.dispose();
      delayed.complete(
        egressResponse(egressPreviewWire(deadline: egressStamp())),
      );
      await preparing;
      expect(c.preview, isNull);
      expect(posts, 1);
    },
  );
}
