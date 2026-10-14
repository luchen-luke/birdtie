import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/city_catalog_recovery.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

Map<String, dynamic> _city(String id) => {
  'id': id,
  'name': '合成恢复城市 $id',
  'region': '单元测试目录',
  'source': {'label': 'fixture', 'maintainer': 'unit', 'freshness': 'test'},
  'map': {
    'provider': 'mapbox',
    'latitude': 57.15,
    'longitude': -2.1,
    'defaultZoom': 12,
  },
};

http.Response _reply(List<Map<String, dynamic>> cities, {int status = 200}) =>
    http.Response(
      jsonEncode({'data': cities}),
      status,
      headers: {'content-type': 'application/json; charset=utf-8'},
    );

class _Catalog {
  _Catalog(
    Future<http.Response> Function(int read) respond, {
    String base = 'https://catalog-fixture.invalid',
  }) {
    client = MockClient((request) async {
      requests.add(request);
      if (request.url.path == '/v1/cities') return respond(++reads);
      return _reply([]);
    });
    city = PublicCityController(client: client, apiBaseUrl: base);
    recovery = CityCatalogRecovery(city);
  }

  int reads = 0;
  final requests = <http.Request>[];
  late final http.Client client;
  late final PublicCityController city;
  late final CityCatalogRecovery recovery;
  bool _closed = false;

  void dispose() {
    if (_closed) return;
    _closed = true;
    recovery.dispose();
    city.dispose();
    client.close();
  }
}

void main() {
  testWidgets('冷启连接失败自动恢复，成功后停止且只读取公开目录', (t) async {
    final catalog = _Catalog((read) async {
      if (read == 1) throw http.ClientException('synthetic connection refused');
      return _reply([_city('aberdeen-gb')]);
    });
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    expect(catalog.reads, 1);
    expect(catalog.city.cityError, '无法读取城市，请稍后重试。');
    await t.pump(const Duration(milliseconds: 1999));
    expect(catalog.reads, 1);
    await t.pump(const Duration(milliseconds: 1));
    expect(catalog.reads, 2);
    expect(catalog.city.cityError, isNull);
    expect(catalog.city.selectedCity?.id, 'aberdeen-gb');
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 2);
    expect(
      catalog.requests.every((request) => request.method == 'GET'),
      isTrue,
    );
    expect(
      catalog.requests.every(
        (request) => !request.headers.containsKey('Authorization'),
      ),
      isTrue,
    );
  });

  testWidgets('持续目录503只在2/5/10秒各自动重读一次并保留手动恢复', (t) async {
    final catalog = _Catalog((_) async => _reply([], status: 503));
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    expect(catalog.reads, 1);
    for (final delay in [2, 5, 10]) {
      final before = catalog.reads;
      await t.pump(Duration(milliseconds: delay * 1000 - 1));
      expect(catalog.reads, before);
      await t.pump(const Duration(milliseconds: 1));
      expect(catalog.reads, before + 1);
    }
    expect(catalog.reads, 4);
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 4);
    expect(catalog.city.cityError, isNotNull);
    await catalog.city.loadCities();
    expect(catalog.reads, 5, reason: '退避预算不禁用已有手动重读');
    catalog.dispose();
  });

  testWidgets('后台取消等待，回到前台失败目录立即重读并恢复', (t) async {
    final catalog = _Catalog((read) async {
      return read == 1
          ? _reply([], status: 503)
          : _reply([_city('aberdeen-gb')]);
    });
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    catalog.recovery.setForeground(false);
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 1);
    catalog.recovery.setForeground(true);
    catalog.recovery.setForeground(true);
    await t.pump();
    expect(catalog.reads, 2);
    expect(catalog.city.selectedCity?.id, 'aberdeen-gb');
    catalog.recovery.setForeground(false);
    catalog.recovery.setForeground(true);
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 2, reason: '成功目录不会因resume重复读取');
  });

  testWidgets('后台初始化不读服务，首次前台继续正常冷启', (t) async {
    final catalog = _Catalog((_) async => _reply([_city('aberdeen-gb')]));
    addTearDown(catalog.dispose);
    catalog.recovery.start(foreground: false);
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 0);
    catalog.recovery.setForeground(true);
    await t.pump();
    expect(catalog.reads, 1);
    expect(catalog.city.selectedCity?.id, 'aberdeen-gb');
  });

  testWidgets('后台完成的失败请求不创建重读定时器，resume可恢复', (t) async {
    final first = Completer<http.Response>();
    final catalog = _Catalog((read) async {
      return read == 1 ? first.future : _reply([_city('aberdeen-gb')]);
    });
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    catalog.recovery.setForeground(false);
    first.complete(_reply([], status: 503));
    await t.pump();
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 1);
    catalog.recovery.setForeground(true);
    await t.pump();
    expect(catalog.reads, 2);
    expect(catalog.city.cityError, isNull);
  });

  testWidgets('手动重读占用中的resume和退避不造成并发目录请求', (t) async {
    final manual = Completer<http.Response>();
    final catalog = _Catalog((read) async {
      return read == 1 ? _reply([], status: 503) : manual.future;
    });
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    final pending = catalog.city.loadCities();
    await t.pump();
    expect(catalog.reads, 2);
    catalog.recovery.setForeground(false);
    catalog.recovery.setForeground(true);
    await t.pump(const Duration(seconds: 3));
    expect(catalog.reads, 2);
    manual.complete(_reply([_city('aberdeen-gb'), _city('edinburgh-gb')]));
    await pending;
    catalog.city.selectCity('edinburgh-gb');
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 2);
    expect(catalog.city.selectedCity?.id, 'edinburgh-gb');
  });

  testWidgets('读目录超时后仍可自动恢复，迟到旧目录不覆盖新城市', (t) async {
    final lateResponse = Completer<http.Response>();
    final catalog = _Catalog((read) async {
      return read == 1 ? lateResponse.future : _reply([_city('aberdeen-gb')]);
    });
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    await t.pump(const Duration(seconds: 10));
    expect(catalog.city.cityError, isNotNull);
    expect(catalog.reads, 1);
    await t.pump(const Duration(seconds: 2));
    expect(catalog.reads, 2);
    expect(catalog.city.selectedCity?.id, 'aberdeen-gb');
    lateResponse.complete(_reply([_city('late-other-city')]));
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 2);
    expect(catalog.city.selectedCity?.id, 'aberdeen-gb');
  });

  testWidgets('自动重读次数耗尽后resume重新给出有限恢复机会', (t) async {
    final catalog = _Catalog((_) async => _reply([], status: 503));
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    for (final delay in [2, 5, 10]) {
      await t.pump(Duration(seconds: delay));
    }
    expect(catalog.reads, 4);
    catalog.recovery.setForeground(false);
    catalog.recovery.setForeground(true);
    await t.pump();
    expect(catalog.reads, 5);
    await t.pump(const Duration(seconds: 2));
    expect(catalog.reads, 6);
    catalog.dispose();
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 6);
  });

  testWidgets('销毁取消待执行重读，不再响应前台或目录变化', (t) async {
    final catalog = _Catalog((_) async => _reply([], status: 503));
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    catalog.recovery.dispose();
    catalog.recovery.dispose();
    catalog.recovery.setForeground(false);
    catalog.recovery.setForeground(true);
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 1);
    await catalog.city.loadCities();
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 2, reason: '已释放listener，手动失败不会重建timer');
  });

  testWidgets('销毁时尚未结束的读取失败不重建定时器', (t) async {
    final pending = Completer<http.Response>();
    final catalog = _Catalog((_) => pending.future);
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    catalog.recovery.dispose();
    pending.complete(_reply([], status: 503));
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 1);
  });

  testWidgets('成功的空目录不作为连接失败自动重读', (t) async {
    final catalog = _Catalog((_) async => _reply([]));
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    await t.pump();
    expect(catalog.city.cityError, isNull);
    expect(catalog.city.cities, isEmpty);
    catalog.recovery.setForeground(false);
    catalog.recovery.setForeground(true);
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 1);
  });

  testWidgets('已有明确城市的成功目录不因start或刷新失败而被重读', (t) async {
    final catalog = _Catalog((read) async {
      return read == 1
          ? _reply([_city('aberdeen-gb'), _city('edinburgh-gb')])
          : _reply([], status: 503);
    });
    addTearDown(catalog.dispose);
    await catalog.city.loadCities();
    catalog.city.selectCity('edinburgh-gb');
    catalog.recovery.start();
    catalog.recovery.start();
    await t.pump();
    expect(catalog.reads, 1);
    await catalog.city.loadCities();
    expect(catalog.reads, 2);
    expect(catalog.city.cityError, isNotNull);
    catalog.recovery.setForeground(false);
    catalog.recovery.setForeground(true);
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 2);
    expect(catalog.city.selectedCity?.id, 'edinburgh-gb');
  });

  testWidgets('缺少API配置不循环请求或创建重读timer', (t) async {
    final catalog = _Catalog((_) async => _reply([]), base: '');
    addTearDown(catalog.dispose);
    catalog.recovery.start();
    catalog.recovery.setForeground(false);
    catalog.recovery.setForeground(true);
    await t.pump(const Duration(minutes: 1));
    expect(catalog.reads, 0);
    expect(catalog.city.cityError, '尚未配置城市服务，请检查运行设置后重试。');
    expect(catalog.city.citiesLoading, isFalse);
  });
}
