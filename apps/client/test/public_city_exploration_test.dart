import 'dart:async';
import 'dart:convert';

import 'package:birdtie_client/src/city/exploration_city_store.dart';
import 'package:birdtie_client/src/city/public_city_controller.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

// Synthetic directory data tests exploration state; it is not a GPS fix or
// verified public Place/source data and never reaches a live service.
Map<String, dynamic> _city(String id, {String provider = 'mapbox'}) => {
  'id': id,
  'name': id,
  'region': '合成单元目录',
  'contentStatus': 'test',
  'source': {'label': 'fixture', 'maintainer': 'unit', 'freshness': 'test'},
  'map': {
    'provider': provider,
    'latitude': 57.15,
    'longitude': -2.1,
    'defaultZoom': 12,
    'sourceRef': 'https://example.invalid/map',
  },
};

class _MemoryCityStore implements ExplorationCityStore {
  String? value;
  Future<String?>? pendingRead;
  bool readFails = false, writeFails = false;
  final writes = <String>[];
  final writeGates = <Completer<void>>[];

  @override
  Future<String?> readCityID() async {
    if (readFails) throw StateError('synthetic read failure');
    return pendingRead == null ? value : await pendingRead;
  }

  @override
  Future<void> writeCityID(String cityID) async {
    writes.add(cityID);
    if (writeGates.isNotEmpty) await writeGates.removeAt(0).future;
    if (writeFails) throw StateError('synthetic write failure');
    value = cityID;
  }
}

class _Catalog {
  _Catalog(this.rows) {
    client = MockClient((r) async {
      requests.add(r);
      return http.Response(
        jsonEncode({'data': r.url.path == '/v1/cities' ? rows : []}),
        r.url.path == '/v1/cities' && failed ? 503 : 200,
        headers: {'content-type': 'application/json; charset=utf-8'},
      );
    });
  }
  List<Map<String, dynamic>> rows;
  bool failed = false;
  final requests = <http.Request>[];
  late final http.Client client;

  PublicCityController controller({ExplorationCityStore? store}) {
    final controller = PublicCityController(
      client: client,
      apiBaseUrl: 'https://directory.example.invalid',
      explorationStore: store,
    );
    addTearDown(controller.dispose);
    return controller;
  }
}

void main() {
  test('Now 002 访客默认 Aberdeen 而非首条上海，不读GPS或写公开范围', () async {
    final catalog = _Catalog([
      _city('shanghai-cn', provider: 'amap'),
      _city('aberdeen-gb'),
    ]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller();
    await controller.loadCities();
    expect(controller.selectedCity?.id, 'aberdeen-gb');
    expect(controller.selectedCity?.map?.defaultZoom, 12);
    expect(catalog.requests.every((r) => r.method == 'GET'), true);
    expect(
      catalog.requests.every((r) => !r.headers.containsKey('Authorization')),
      true,
    );
    expect(
      catalog.requests.every(
        (r) =>
            r.url.path == '/v1/cities' ||
            r.url.path.startsWith('/v1/cities/aberdeen-gb/'),
      ),
      true,
    );
  });

  test('无默认城市时选择可用城市级底图，不选未知provider或未配置高德', () async {
    final catalog = _Catalog([
      _city('shanghai-cn', provider: 'amap'),
      _city('unknown', provider: 'unknown'),
      _city('edinburgh-gb'),
    ]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller();
    await controller.loadCities();
    expect(controller.selectedCity?.id, 'edinburgh-gb');
  });

  test('明确手动选城在新controller恢复，默认探索不自动写本机', () async {
    final store = _MemoryCityStore();
    final catalog = _Catalog([_city('aberdeen-gb'), _city('edinburgh-gb')]);
    addTearDown(catalog.client.close);
    final first = catalog.controller(store: store);
    await first.loadCities();
    await first.explorationRestoreSettled;
    expect(first.selectedCity?.id, 'aberdeen-gb');
    expect(store.writes, isEmpty);
    first.selectCity('edinburgh-gb');
    await first.explorationWritesSettled;
    final restored = catalog.controller(store: store);
    await restored.loadCities();
    await restored.explorationRestoreSettled;
    expect(restored.selectedCity?.id, 'edinburgh-gb');
    expect(store.writes, ['edinburgh-gb']);
  });

  test('本机读失败仍有默认城市且提供说明', () async {
    final store = _MemoryCityStore()..readFails = true;
    final catalog = _Catalog([_city('aberdeen-gb')]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    await controller.explorationRestoreSettled;
    expect(controller.selectedCity?.id, 'aberdeen-gb');
    expect(controller.explorationNotice, contains('未能恢复'));
    expect(controller.cityError, isNull);
  });

  test('本机读取未返回时默认地图不等待，迟到读取不能覆盖新手动选择', () async {
    final read = Completer<String?>();
    final store = _MemoryCityStore()..pendingRead = read.future;
    final catalog = _Catalog([
      _city('aberdeen-gb'),
      _city('edinburgh-gb'),
      _city('dundee-gb'),
    ]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    expect(controller.selectedCity?.id, 'aberdeen-gb');
    controller.selectCity('dundee-gb');
    read.complete('edinburgh-gb');
    await controller.explorationRestoreSettled;
    await controller.explorationWritesSettled;
    expect(controller.selectedCity?.id, 'dundee-gb');
    expect(store.value, 'dundee-gb');
  });

  test('手动确认当前默认城市也阻止迟到旧城市恢复', () async {
    final read = Completer<String?>();
    final store = _MemoryCityStore()..pendingRead = read.future;
    final catalog = _Catalog([_city('aberdeen-gb'), _city('edinburgh-gb')]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    controller.selectCity('aberdeen-gb');
    read.complete('edinburgh-gb');
    await controller.explorationRestoreSettled;
    await controller.explorationWritesSettled;
    expect(controller.selectedCity?.id, 'aberdeen-gb');
    expect(store.value, 'aberdeen-gb');
  });

  test('旧本机选择可以替换自动默认，但不能写成新的GPS观察', () async {
    final read = Completer<String?>();
    final store = _MemoryCityStore()..pendingRead = read.future;
    final catalog = _Catalog([_city('aberdeen-gb'), _city('edinburgh-gb')]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    read.complete('edinburgh-gb');
    await controller.explorationRestoreSettled;
    expect(controller.selectedCity?.id, 'edinburgh-gb');
    expect(store.writes, isEmpty);
    expect(catalog.requests.every((r) => r.method == 'GET'), true);
  });

  test('本机写失败不回滚当前选城，不拦截后续选择', () async {
    final store = _MemoryCityStore()..writeFails = true;
    final catalog = _Catalog([_city('aberdeen-gb'), _city('edinburgh-gb')]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    await controller.explorationRestoreSettled;
    controller.selectCity('edinburgh-gb');
    await controller.explorationWritesSettled;
    expect(controller.selectedCity?.id, 'edinburgh-gb');
    expect(controller.explorationNotice, contains('未能保存'));
    store.writeFails = false;
    controller.selectCity('aberdeen-gb');
    await controller.explorationWritesSettled;
    expect(store.value, 'aberdeen-gb');
    expect(controller.explorationNotice, isNull);
  });

  test('快速手动切城串行保存，最终恢复最后一次选择', () async {
    final gate = Completer<void>();
    final store = _MemoryCityStore()..writeGates.add(gate);
    final catalog = _Catalog([
      _city('aberdeen-gb'),
      _city('edinburgh-gb'),
      _city('dundee-gb'),
    ]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    await controller.explorationRestoreSettled;
    controller.selectCity('edinburgh-gb');
    await Future<void>.delayed(Duration.zero);
    controller.selectCity('dundee-gb');
    expect(store.writes, ['edinburgh-gb']);
    gate.complete();
    await controller.explorationWritesSettled;
    expect(store.writes, ['edinburgh-gb', 'dundee-gb']);
    expect(store.value, 'dundee-gb');
    expect(controller.selectedCity?.id, 'dundee-gb');
  });

  test('临时Task城市不会覆盖永久探索选择或让旧restore覆盖当前视图', () async {
    final store = _MemoryCityStore()..value = 'edinburgh-gb';
    final catalog = _Catalog([
      _city('aberdeen-gb'),
      _city('edinburgh-gb'),
      _city('dundee-gb'),
    ]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    await controller.explorationRestoreSettled;
    controller.selectCity('dundee-gb', rememberExploration: false);
    await controller.explorationWritesSettled;
    expect(controller.selectedCity?.id, 'dundee-gb');
    expect(store.value, 'edinburgh-gb');
    expect(store.writes, isEmpty);
    final restarted = catalog.controller(store: store);
    await restarted.loadCities();
    await restarted.explorationRestoreSettled;
    expect(restarted.selectedCity?.id, 'edinburgh-gb');
  });

  test('目录移除已选城市时清除旧投影并恢复可用默认且解释', () async {
    final store = _MemoryCityStore()..value = 'edinburgh-gb';
    final catalog = _Catalog([_city('aberdeen-gb'), _city('edinburgh-gb')]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    await controller.explorationRestoreSettled;
    catalog.rows = [_city('aberdeen-gb')];
    await controller.loadCities();
    expect(controller.selectedCity?.id, 'aberdeen-gb');
    expect(controller.explorationNotice, contains('目录移除'));
    expect(controller.places, isEmpty);
    expect(controller.activities, isEmpty);
    expect(controller.organizationPins, isEmpty);
    expect(store.value, 'edinburgh-gb', reason: '临时回退不覆盖本人永久选城');
  });

  test('保存的国内城市provider未配置时回退，但明确手动选择仍可进入错误恢复', () async {
    final store = _MemoryCityStore()..value = 'shanghai-cn';
    final catalog = _Catalog([
      _city('shanghai-cn', provider: 'amap'),
      _city('aberdeen-gb'),
    ]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    await controller.explorationRestoreSettled;
    expect(controller.selectedCity?.id, 'aberdeen-gb');
    expect(controller.explorationNotice, contains('暂不可用'));
    controller.selectCity('shanghai-cn');
    expect(controller.selectedCity?.id, 'shanghai-cn');
    await controller.explorationWritesSettled;
    expect(store.value, 'shanghai-cn');
  });

  test('目录暂时失败保留现有城市与手动选择，不伪造目录成功', () async {
    final catalog = _Catalog([_city('aberdeen-gb'), _city('edinburgh-gb')]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller();
    await controller.loadCities();
    controller.selectCity('edinburgh-gb');
    catalog.failed = true;
    await controller.loadCities();
    expect(controller.selectedCity?.id, 'edinburgh-gb');
    expect(controller.cityError, isNotNull);
  });

  test('目录重新出现恢复原明确选择，临时回退不成为永久城市', () async {
    final store = _MemoryCityStore()..value = 'edinburgh-gb';
    final catalog = _Catalog([_city('aberdeen-gb')]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    await controller.explorationRestoreSettled;
    expect(controller.selectedCity?.id, 'aberdeen-gb');
    catalog.rows = [_city('aberdeen-gb'), _city('edinburgh-gb')];
    await controller.loadCities();
    expect(controller.selectedCity?.id, 'edinburgh-gb');
    expect(controller.explorationNotice, isNull);
    expect(store.writes, isEmpty);
  });

  test('目录恢复不能覆盖回退期间的新手动城市', () async {
    final store = _MemoryCityStore()..value = 'edinburgh-gb';
    final catalog = _Catalog([_city('aberdeen-gb'), _city('dundee-gb')]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    await controller.explorationRestoreSettled;
    controller.selectCity('dundee-gb');
    await controller.explorationWritesSettled;
    catalog.rows = [
      _city('aberdeen-gb'),
      _city('edinburgh-gb'),
      _city('dundee-gb'),
    ];
    await controller.loadCities();
    expect(controller.selectedCity?.id, 'dundee-gb');
    expect(store.value, 'dundee-gb');
  });

  test('超时的旧本机读取不能在超时后改城市', () async {
    final read = Completer<String?>();
    final store = _MemoryCityStore()..pendingRead = read.future;
    final catalog = _Catalog([_city('aberdeen-gb'), _city('edinburgh-gb')]);
    addTearDown(catalog.client.close);
    final controller = catalog.controller(store: store);
    await controller.loadCities();
    expect(controller.selectedCity?.id, 'aberdeen-gb');
    await controller.explorationRestoreSettled;
    expect(controller.explorationNotice, contains('未能恢复'));
    read.complete('edinburgh-gb');
    await Future<void>.delayed(Duration.zero);
    expect(controller.selectedCity?.id, 'aberdeen-gb');
  });

  test('已销毁controller忽略迟到恢复，不向新工作区通知', () async {
    final read = Completer<String?>();
    final store = _MemoryCityStore()..pendingRead = read.future;
    final catalog = _Catalog([_city('aberdeen-gb'), _city('edinburgh-gb')]);
    addTearDown(catalog.client.close);
    final controller = PublicCityController(
      client: catalog.client,
      apiBaseUrl: 'https://directory.example.invalid',
      explorationStore: store,
    );
    await controller.loadCities();
    var notices = 0;
    controller.addListener(() => notices++);
    controller.dispose();
    read.complete('edinburgh-gb');
    await controller.explorationRestoreSettled;
    expect(notices, 0);
    expect(controller.selectedCity?.id, 'aberdeen-gb');
    expect(store.writes, isEmpty);
  });
}
