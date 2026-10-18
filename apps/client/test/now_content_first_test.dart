import 'dart:convert';

import 'package:birdtie_client/src/workspace/moment_interaction.dart';
import 'package:birdtie_client/src/workspace/now_map_content_panel.dart';
import 'package:birdtie_client/src/workspace/owned_map_api.dart';
import 'package:birdtie_client/src/workspace/owned_map_controller.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _owner = '11111111-1111-4111-8111-111111111111';
const _seed = '22222222-2222-4222-8222-222222222222';
const _first = '33333333-3333-4333-8333-333333333333';
const _last = '44444444-4444-4444-8444-444444444444';
const _placeA = '55555555-5555-4555-8555-555555555555';
const _placeB = '66666666-6666-4666-8666-666666666666';
const _threadID = '77777777-7777-4777-8777-777777777777';
const _comment = '88888888-8888-4888-8888-888888888888';
const _origin = 'https://now-content.test';

http.Response _response(Object data) => http.Response.bytes(
  utf8.encode(jsonEncode({'data': data})),
  200,
  headers: {'content-type': 'application/json'},
);

Map<String, Object?> _lease() {
  final now = DateTime.now().toUtc();
  return {
    'observedAt': now.toIso8601String(),
    'validUntil': now.add(const Duration(seconds: 30)).toIso8601String(),
  };
}

class _PanelController extends OwnedMapController {
  _PanelController(this.changes, {this.threaded = false})
    : super(
        client: MockClient(
          (_) async => throw StateError('unexpected map HTTP'),
        ),
        apiBaseUrl: () => _origin,
        ownerID: () => _owner,
        authorizationHeader: () => 'Bearer synthetic',
        organizationWorkspaceID: () => null,
        identityChanges: changes,
        refreshAllowed: () => true,
      );
  final ValueNotifier<int> changes;
  final bool threaded;
  bool permitted = true;
  final places = <(String?, String?, String?)>[];
  final threads = <String>[];
  late final pin = OwnedMapPin(
    placeID: '',
    name: '原动态',
    categoryCode: 'moment',
    cityID: null,
    latitude: 57.1,
    longitude: -2.1,
    visibilitySources: const ['BIRDTIE_PUBLIC_MOMENT'],
    sourceVersion: 'a' * 64,
    momentCount: 1,
    moments: [
      OwnedMapMoment(
        _seed,
        2,
        'BIRDTIE_PUBLIC_MOMENT',
        '/v1/me/map-content/moments/$_seed',
      ),
    ],
    momentsTruncated: false,
    cover: null,
    kind: 'MOMENT_ANCHOR',
    markerID: 'anchor:$_placeA',
    anchorID: _placeA,
    momentID: _seed,
    entryID: _seed,
    threadID: threaded ? _threadID : '',
  );
  OwnedMapMomentDetail detail(String id) => OwnedMapMomentDetail.decode(
    {
      'schemaVersion': 'map-content-moment-v1',
      'viewerAccountId': _owner,
      'id': id,
      'revision': 2,
      'visibilitySource': 'BIRDTIE_PUBLIC_MOMENT',
      'canEdit': false,
      'status': 'published',
      'visibility': 'public',
      'title': '',
      'body': id == _seed
          ? (threaded ? '中段无地点正文' : '原动态正文')
          : id == _first
          ? '第一段正文'
          : id == _last
          ? '远页实际动态'
          : '合法地点动态$id',
      'createdAt': DateTime.now()
          .toUtc()
          .subtract(const Duration(days: 1))
          .toIso8601String(),
      'publishedAt': DateTime.now()
          .toUtc()
          .subtract(const Duration(hours: 1))
          .toIso8601String(),
      'createdAtSource': 'NATIVE',
      'locations': threaded && id == _seed
          ? <Object>[]
          : [
              {'placeId': _placeA, 'name': '明确地点甲', 'anchor': null},
              {'placeId': _placeB, 'name': '明确地点乙', 'anchor': null},
            ],
      'media': <Object>[],
      'sourceVersion': 'b' * 64,
      ..._lease(),
    },
    ownerID: _owner,
    now: DateTime.now().toUtc(),
  );
  @override
  bool get current => permitted;
  @override
  OwnedMapPin? find(String id) => id == pin.mapID ? pin : null;
  @override
  bool sourceCurrent(OwnedMapPin pin) => permitted;
  @override
  bool detailCurrent(OwnedMapMomentDetail value) =>
      permitted && value.validUntil.isAfter(DateTime.now().toUtc());
  @override
  bool threadCurrent(OwnedMapThread value) => permitted && value.current;
  @override
  Future<OwnedMapMomentDetail> readMoment(
    OwnedMapMoment value, {
    bool Function()? current,
    Future<void>? abortTrigger,
  }) async {
    if (!permitted || current?.call() == false)
      throw const FormatException('retired');
    return detail(value.id);
  }

  @override
  Future<OwnedMapThread?> readThread(
    OwnedMapPin pin,
    OwnedMapMomentDetail seed,
  ) async {
    threads.add(seed.id);
    if (!threaded) return null;
    final lease = _lease();
    Map<String, Object?> wire(String id) {
      final d = detail(id);
      return {
        'schemaVersion': 'map-content-moment-v1',
        'viewerAccountId': _owner,
        'id': id,
        'revision': d.revision,
        'visibilitySource': d.visibilitySource,
        'canEdit': false,
        'status': d.status,
        'visibility': d.visibility,
        'title': '',
        'body': d.body,
        'createdAt': d.createdAt.toIso8601String(),
        'locations': <Object>[],
        'media': <Object>[],
        'sourceVersion': d.sourceVersion,
        ...lease,
      };
    }

    return OwnedMapThread.decode(
      {
        'schemaVersion': 'map-content-thread-v1',
        'viewerAccountId': _owner,
        'seedMomentId': seed.id,
        'seedRevision': 2,
        'seedVisibilitySource': 'BIRDTIE_PUBLIC_MOMENT',
        'status': 'AVAILABLE',
        'threadId': _threadID,
        'entries': [
          {'position': 0, 'status': 'AVAILABLE', 'detail': wire(_first)},
          {'position': 1, 'status': 'UNAVAILABLE', 'detail': null},
          {'position': 2, 'status': 'AVAILABLE', 'detail': wire(_seed)},
          {'position': 3, 'status': 'AVAILABLE', 'detail': wire(_last)},
        ],
        'sourceVersion': 'c' * 64,
        ...lease,
      },
      ownerID: _owner,
      seedMomentID: seed.id,
      now: DateTime.now().toUtc(),
    );
  }

  @override
  Future<OwnedMapPlacePage> readPlaceMoments(
    OwnedMapPin pin, {
    String? cursor,
    String? placeID,
    OwnedMapMomentDetail? relationSource,
  }) async {
    places.add((placeID, relationSource?.id, cursor));
    if (placeID != _placeB || relationSource?.id != _seed)
      throw const FormatException('exact explicit relation required');
    return OwnedMapPlacePage(
      placeID: _placeB,
      items: cursor == null
          ? [
              detail(_seed),
              for (var i = 1; i < 20; i++)
                detail(
                  '99999999-9999-4999-8999-${i.toString().padLeft(12, '0')}',
                ),
            ]
          : [detail(_last)],
      nextCursor: cursor == null ? 'actual-next-page' : null,
      observedAt: DateTime.now().toUtc(),
      validUntil: DateTime.now().toUtc().add(const Duration(seconds: 30)),
      sourceVersion: 'd' * 64,
    );
  }
}

Future<void> _reads(WidgetTester t) async {
  for (var i = 0; i < 8; i++) {
    await t.pump(const Duration(milliseconds: 20));
  }
}

http.Client _comments(List<String> reads, {int revision = 2}) =>
    MockClient((r) async {
      if (r.method != 'GET' || !r.url.path.endsWith('/interactions'))
        throw StateError('unexpected comment write');
      final id = r.url.pathSegments[r.url.pathSegments.length - 2];
      reads.add(id);
      return _response({
        'schemaVersion': 'moment-interaction-page-v2',
        'momentId': id,
        'momentRevision': revision,
        'canComment': true,
        'likeCount': 0,
        'commentCount': 1,
        'liked': false,
        'likeVersion': 0,
        'comments': [
          {
            'id': _comment,
            'body': '可以明确回复的原评论',
            'createdAt': DateTime.now().toUtc().toIso8601String(),
            'authorId': null,
            'authorPublicProfile': null,
            'canDelete': false,
            'version': 1,
            'canReply': true,
            'replyTo': null,
          },
        ],
        'nextCursor': null,
        'sourceVersion': 'e' * 64,
        ..._lease(),
      });
    });

Widget _panel(
  _PanelController c,
  http.Client client,
  void Function(
    MomentInteractionController?,
    OwnedMapMomentDetail?,
    MomentComment?,
  )
  target, {
  bool expanded = true,
  bool reduced = false,
}) => MaterialApp(
  home: Scaffold(
    body: Builder(
      builder: (context) => MediaQuery(
        data: MediaQuery.of(context).copyWith(disableAnimations: reduced),
        child: Align(
          alignment: Alignment.topLeft,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 390, maxHeight: 600),
            child: NowMapContentPanel(
              controller: c,
              pin: c.pin,
              current: () => c.permitted,
              changes: c.changes,
              onClose: () {},
              onExpand: () {},
              onEdit: (_, __) {},
              initialExpanded: expanded,
              interactionClient: client,
              onCommentTarget: target,
            ),
          ),
        ),
      ),
    ),
  ),
);

void main() {
  const storage = MethodChannel('plugins.it_nomads.com/flutter_secure_storage');
  setUp(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(storage, (_) async => null),
  );
  tearDown(
    () => TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(storage, null),
  );

  testWidgets(
    'Now reply uses host input and source retirement keeps exact disabled target',
    (t) async {
      final changes = ValueNotifier(0), requests = <String>[];
      final c = _PanelController(changes), client = _comments(requests);
      MomentInteractionController? targetController;
      OwnedMapMomentDetail? targetMoment;
      MomentComment? targetReply;
      await t.pumpWidget(
        _panel(c, client, (controller, moment, reply) {
          targetController = controller;
          targetMoment = moment;
          targetReply = reply;
        }),
      );
      await _reads(t);
      expect(targetMoment?.id, _seed);
      expect(targetController?.canComment, true);
      expect(find.byType(TextField), findsNothing);
      expect(requests, [_seed]);
      await t.ensureVisible(find.text('回复').first);
      await t.tap(find.text('回复').first);
      await _reads(t);
      expect(targetReply?.id, _comment);
      final original = targetController!;
      c.permitted = false;
      changes.value++;
      await _reads(t);
      expect(targetController, isNull);
      expect(targetMoment?.id, _seed);
      expect(targetReply?.id, _comment);
      expect(original.canComment, false);
      expect(find.text('原动态正文'), findsNothing);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      changes.dispose();
      client.close();
    },
  );

  testWidgets(
    'explicit Place query pages beyond pin selectors and returns original content',
    (t) async {
      final changes = ValueNotifier(0), requests = <String>[];
      final c = _PanelController(changes), client = _comments(requests);
      OwnedMapMomentDetail? target;
      await t.pumpWidget(_panel(c, client, (_, moment, __) => target = moment));
      await _reads(t);
      expect(
        find.byKey(const ValueKey('moment-place:$_seed:$_placeA')),
        findsOneWidget,
      );
      await t.tap(find.byKey(const ValueKey('moment-place:$_seed:$_placeB')));
      await _reads(t);
      expect(target, isNull);
      expect(c.places, [(_placeB, _seed, null)]);
      expect(find.text('这里的动态 · 明确地点乙'), findsOneWidget);
      await t.scrollUntilVisible(
        find.text('加载更多动态'),
        500,
        scrollable: find.byType(Scrollable).first,
        maxScrolls: 12,
      );
      await t.tap(find.text('加载更多动态'));
      await _reads(t);
      expect(c.places.last, (_placeB, _seed, 'actual-next-page'));
      await t.scrollUntilVisible(
        find.text('远页实际动态'),
        500,
        scrollable: find.byType(Scrollable).first,
        maxScrolls: 12,
      );
      expect(find.text('远页实际动态'), findsOneWidget);
      expect(c.pin.moments.length, 1);
      await t.tap(find.byTooltip('返回原动态'));
      await _reads(t);
      expect(find.text('原动态正文'), findsOneWidget);
      expect(target?.id, _seed);
      expect(find.byType(TextField), findsNothing);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      changes.dispose();
      client.close();
    },
  );

  testWidgets(
    'authoritative middle entry without location retains gaps and explicit current Moment',
    (t) async {
      final changes = ValueNotifier(0), requests = <String>[];
      final c = _PanelController(changes, threaded: true),
          client = _comments(requests);
      OwnedMapMomentDetail? target;
      await t.pumpWidget(_panel(c, client, (_, moment, __) => target = moment));
      await _reads(t);
      expect(find.text('中段无地点正文'), findsOneWidget);
      expect(target?.id, _seed);
      expect(find.text('串文 · 4段'), findsOneWidget);
      expect(c.threads, [_seed]);
      await t.drag(find.byType(CustomScrollView), const Offset(0, 420));
      await _reads(t);
      final first = find.byKey(const ValueKey('comment-target:$_first'));
      await t.ensureVisible(first);
      await t.tap(first);
      await _reads(t);
      expect(target?.id, _first);
      expect(find.text('第2段暂不可查看'), findsOneWidget);
      expect(c.threads, [_seed]);
      expect(find.byType(TextField), findsNothing);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      changes.dispose();
      client.close();
    },
  );

  testWidgets(
    'reader keeps its viewport stable after expansion and reduced-motion updates',
    (t) async {
      final changes = ValueNotifier(0), c = _PanelController(changes);
      final client = _comments([]);
      await t.pumpWidget(_panel(c, client, (_, __, ___) {}, expanded: false));
      await _reads(t);
      final panel = find.byType(NowMapContentPanel);
      final element = t.element(panel);
      expect(t.getSize(panel).height, 288);
      await t.tap(find.text('查看动态'));
      await t.pump();
      expect(t.getSize(panel).height, 600);
      final viewport = t.getSize(find.byType(ListView));
      await t.pump(const Duration(milliseconds: 100));
      expect(t.getSize(find.byType(ListView)), viewport);
      await t.pump(const Duration(milliseconds: 260));
      expect(t.getSize(find.byType(ListView)), viewport);
      expect(t.element(panel), same(element));
      await t.pumpWidget(
        _panel(c, client, (_, __, ___) {}, expanded: false, reduced: true),
      );
      await _reads(t);
      expect(t.element(panel), same(element));
      expect(t.getSize(find.byType(ListView)), viewport);
      expect(find.byType(TextField), findsNothing);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      changes.dispose();
      client.close();
    },
  );

  testWidgets(
    'newer interaction revision cannot approve a comment on older displayed body',
    (t) async {
      final changes = ValueNotifier(0), requests = <String>[];
      final c = _PanelController(changes),
          client = _comments(requests, revision: 3);
      MomentInteractionController? targetController;
      OwnedMapMomentDetail? target;
      await t.pumpWidget(
        _panel(c, client, (controller, moment, _) {
          targetController = controller;
          target = moment;
        }),
      );
      await _reads(t);
      expect(target?.id, _seed);
      expect(target?.revision, 2);
      expect(targetController, isNull);
      expect(requests, [_seed]);
      expect(find.text('动态版本已变化，请重新读取正文后再评论'), findsOneWidget);
      expect(find.byType(TextField), findsNothing);
      await t.pumpWidget(const SizedBox());
      c.dispose();
      changes.dispose();
      client.close();
    },
  );
}
