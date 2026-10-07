import 'dart:async';
import 'package:birdtie_client/src/workspace/online_social_opportunity_api.dart';
import 'package:birdtie_client/src/workspace/online_social_opportunity_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'online_social_opportunity_api_test.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('本人选择真实意图→当前结果→详情前重读，无任何自动写', () async {
    final methods = <String>[];
    final c = OnlineSocialOpportunityController(
      api: OnlineSocialOpportunityAPI(
        client: MockClient((r) async {
          methods.add(r.method);
          return onlineOppResponse(
            onlineOppWire(options: r.url.path.endsWith('/options')),
          );
        }),
      ),
      identity: () => const ('Bearer A', onlineOppOwner, null),
    );
    await c.load();
    expect(c.view, isNull);
    await c.select(onlineOppIntent);
    expect(c.view!.items.single.sourceID, onlineOppSource);
    final fresh = await c.resolve(c.view!.items.single);
    expect(fresh!.sourceID, onlineOppSource);
    expect(methods, ['GET', 'GET', 'GET']);
    c.dispose();
  });
  test('同opaqueToken账号A-B-A永久退休缓存与迟到，不自动重读', () async {
    OnlineDiscoveryIdentity who = ('Bearer fixed', onlineOppOwner, null);
    final late = Completer<http.Response>();
    var count = 0;
    final c = OnlineSocialOpportunityController(
      api: OnlineSocialOpportunityAPI(
        client: MockClient((r) async {
          count++;
          if (count == 3) return late.future;
          return onlineOppResponse(
            onlineOppWire(options: r.url.path.endsWith('/options')),
          );
        }),
      ),
      identity: () => who,
    );
    await c.load();
    await c.select(onlineOppIntent);
    final pending = c.select(onlineOppIntent);
    who = ('Bearer fixed', onlineOppSource, null);
    c.sync();
    who = ('Bearer fixed', onlineOppOwner, null);
    c.sync();
    late.complete(onlineOppResponse(onlineOppWire()));
    await pending;
    expect(c.current, false);
    expect(c.view, isNull);
    expect(c.options, isNull);
    await c.load();
    expect(count, 3);
    c.dispose();
  });
  test('403旧缓存撤回，自然有效期到期不继续操作，未读身份零请求', () async {
    var phase = 0;
    final c = OnlineSocialOpportunityController(
      api: OnlineSocialOpportunityAPI(
        client: MockClient((r) async {
          if (phase == 1) return http.Response('{}', 403);
          return onlineOppResponse(
            onlineOppWire(options: r.url.path.endsWith('/options')),
          );
        }),
      ),
      identity: () => const ('Bearer A', onlineOppOwner, null),
    );
    await c.load();
    await c.select(onlineOppIntent);
    phase = 1;
    await c.resolve(c.view!.items.single);
    expect(c.view, isNull);
    expect(c.error, isNotNull);
    c.dispose();
    var requests = 0;
    final anonymous = OnlineSocialOpportunityController(
      api: OnlineSocialOpportunityAPI(
        client: MockClient((r) async {
          requests++;
          return onlineOppResponse(onlineOppWire(options: true));
        }),
      ),
      identity: () => const (null, null, null),
    );
    await anonymous.load();
    expect(requests, 0);
    anonymous.dispose();
  });
  test('源版本变化不继承旧卡动作，不外发；GET未知无隐式重试', () async {
    var reads = 0;
    final c = OnlineSocialOpportunityController(
      api: OnlineSocialOpportunityAPI(
        client: MockClient((r) async {
          reads++;
          if (reads == 4) throw Exception('lost GET');
          final w = onlineOppWire(options: r.url.path.endsWith('/options'));
          if (reads == 3) {
            (w['items'] as List).single['sourceVersion'] =
                '2026-01-01T12:00:01Z';
          }
          return onlineOppResponse(w);
        }),
      ),
      identity: () => const ('Bearer A', onlineOppOwner, null),
    );
    await c.load();
    await c.select(onlineOppIntent);
    final old = c.view!.items.single;
    expect(await c.resolve(old), isNull);
    await c.select(onlineOppIntent);
    expect(reads, 4);
    expect(c.view, isNull);
    c.dispose();
  });
  test('实际finite旧来源不会因收到过期回包复活', () async {
    final c = OnlineSocialOpportunityController(
      api: OnlineSocialOpportunityAPI(
        client: MockClient(
          (r) async => onlineOppResponse(
            onlineOppWire(
              options: true,
              now: DateTime.now().subtract(const Duration(minutes: 3)),
            ),
          ),
        ),
      ),
      identity: () => const ('Bearer A', onlineOppOwner, null),
    );
    await c.load();
    expect(c.options, isNull);
    expect(c.error, isNotNull);
    c.dispose();
  });
}
