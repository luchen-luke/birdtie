import 'dart:convert';
import 'package:http/http.dart' as http;
import '../content/moment_control.dart';
import 'human_causal_control.dart';
import 'public_moment_media.dart';
import 'entity_cover.dart';

class MomentThreadDraftEntry {
  MomentThreadDraftEntry({
    required this.localID,
    required this.cityID,
    this.placeID = '',
    this.text = '',
    List<PublicMomentMediaSelection> media = const [],
  }) : media = List.unmodifiable(media);
  final String localID, cityID, placeID, text;
  final List<PublicMomentMediaSelection> media;
  MomentThreadDraftEntry copy({
    String? cityID,
    String? placeID,
    String? text,
    List<PublicMomentMediaSelection>? media,
  }) => MomentThreadDraftEntry(
    localID: localID,
    cityID: cityID ?? this.cityID,
    placeID: placeID ?? this.placeID,
    text: text ?? this.text,
    media: media ?? this.media,
  );
  String get title => '';

  Map<String, Object?> get input => {
    'cityId': cityID,
    'placeId': placeID,
    'title': title,
    'body': text.trim(),
    'occurredAt': null,
    'timePrecision': 'unknown',
    'locationPrecision': placeID.isNotEmpty
        ? 'place'
        : (cityID.isEmpty ? 'none' : 'city'),
  };
  MomentControlInput _controlInput(String localScope) => MomentControlInput(
    operationID: localID,
    action: 'PUBLISH',
    momentID: localID,
    expectedRevision: 1,
    input: input,
    media: media.isEmpty ? null : media,
    localScope: media.isEmpty ? '' : localScope,
  );

  Map<String, Object?> toItem({String localScope = ''}) {
    if (text.trim().isEmpty && media.isEmpty) {
      throw const FormatException('每条动态需要正文或已准备材料');
    }
    _controlInput(localScope);
    if (media.length > 9 ||
        media.any(
          (image) =>
              !humanControlID(image.mediaID) ||
              !humanControlVersion(image.version, zero: false) ||
              !image.rights.valid,
        )) {
      throw const FormatException('串文材料不符或已到期');
    }
    return {
      'input': input,
      'media': media.map((image) => image.toJson()).toList(),
    };
  }

  Map<String, Object?> get json => {
    'localId': localID,
    'cityId': cityID,
    'placeId': placeID,
    'text': text,
    'media': media.map((image) => image.toJson()).toList(),
  };
  factory MomentThreadDraftEntry.decode(Object? raw) {
    final value = humanControlObject(raw, {
      'localId',
      'cityId',
      'placeId',
      'text',
      'media',
    });
    if (!humanControlID(value['localId']) ||
        value['cityId'] is! String ||
        utf8.encode(value['cityId']).length > 80 ||
        value['placeId'] is! String ||
        value['placeId'] != '' && !humanControlID(value['placeId']) ||
        value['text'] is! String ||
        utf8.encode(value['text']).length > 6000 ||
        value['media'] is! List ||
        (value['media'] as List).length > 9) {
      throw const FormatException('串文私人草稿不符');
    }
    return MomentThreadDraftEntry(
      localID: value['localId'],
      cityID: value['cityId'],
      placeID: value['placeId'],
      text: value['text'],
      media: (value['media'] as List).map((raw) {
        final media = humanControlObject(raw, {
          'mediaId',
          'mediaVersion',
          'rights',
        });
        final r = humanControlObject(media['rights'], {
          'basis',
          'attribution',
          'licenseReference',
          'retentionUntil',
          'canRetain',
          'canPublish',
        });
        if (!humanControlID(media['mediaId']) ||
            !humanControlVersion(media['mediaVersion'], zero: false) ||
            r['basis'] is! String ||
            r['attribution'] is! String ||
            r['licenseReference'] is! String ||
            r['canRetain'] is! bool ||
            r['canPublish'] is! bool) {
          throw const FormatException('串文材料选择不符');
        }
        return PublicMomentMediaSelection(
          media['mediaId'],
          media['mediaVersion'],
          EntityCoverRights(
            basis: r['basis'],
            attribution: r['attribution'],
            licenseReference: r['licenseReference'],
            retentionUntil: humanControlTime(r['retentionUntil']),
            canRetain: r['canRetain'],
            canPublish: r['canPublish'],
          ),
        );
      }).toList(),
    );
  }
}

class MomentThreadInput {
  MomentThreadInput({
    required this.operationID,
    required this.action,
    this.threadID = '',
    this.localScope = '',
    this.expectedVersion = 0,
    List<MomentThreadDraftEntry> entries = const [],
  }) : entries = List.unmodifiable(entries);
  final String operationID, action, threadID, localScope;
  final int expectedVersion;
  final List<MomentThreadDraftEntry> entries;
  void validate() {
    if (!humanControlID(operationID) ||
        !humanControlVersion(expectedVersion) ||
        expectedVersion >= humanControlMaxVersion) {
      throw const FormatException('串文操作不符');
    }
    if (action == 'CREATE_PUBLISH') {
      if (threadID.isNotEmpty ||
          expectedVersion != 0 ||
          !humanControlID(localScope) ||
          entries.length < 2 ||
          entries.length > 20) {
        throw const FormatException('请保留2至20条有序内容');
      }
      final seen = <String>{};
      for (final entry in entries) {
        entry.toItem(localScope: localScope);
        for (final image in entry.media) {
          if (!seen.add(image.mediaID)) {
            throw const FormatException('同一材料不能重复分配到多条');
          }
        }
      }
    } else if (action != 'WITHDRAW' ||
        !humanControlID(threadID) ||
        expectedVersion < 1 ||
        localScope.isNotEmpty ||
        entries.isNotEmpty) {
      throw const FormatException('整串撤回输入不符');
    }
  }

  Map<String, Object?> toJson() {
    validate();
    return {
      'operationId': operationID,
      'action': action,
      'threadId': threadID,
      'localScope': localScope,
      'audience': 'PUBLIC',
      'expectedVersion': expectedVersion,
      'items': entries
          .map((entry) => entry.toItem(localScope: localScope))
          .toList(),
    };
  }

  String get digest {
    validate();
    return momentHash([
      'moment-thread-v1',
      operationID,
      action,
      threadID,
      localScope,
      'PUBLIC',
      expectedVersion,
      entries
          .map(
            (entry) => [
              entry._controlInput(localScope).inputHash,
              entry.media
                  .map(
                    (image) => [
                      image.mediaID,
                      image.version,
                      image.rights.digest,
                    ],
                  )
                  .toList(),
            ],
          )
          .toList(),
    ]);
  }
}

class MomentThreadMember {
  const MomentThreadMember(
    this.momentID,
    this.status,
    this.position,
    this.revision,
    this.mediaSetVersion,
    this.media,
  );
  final String momentID, status;
  final int position, revision, mediaSetVersion;
  final List<Map<String, dynamic>> media;
  factory MomentThreadMember.decode(Object? raw, int index) {
    final value = humanControlObject(raw, {
      'momentId',
      'status',
      'position',
      'appliedRevision',
      'mediaSetVersion',
      'media',
    });
    if (!humanControlID(value['momentId']) ||
        !const {'published', 'withdrawn'}.contains(value['status']) ||
        value['position'] != index ||
        !humanControlVersion(value['appliedRevision'], zero: false) ||
        value['appliedRevision'] < 2 ||
        !humanControlVersion(value['mediaSetVersion']) ||
        value['media'] is! List ||
        (value['media'] as List).length > 9) {
      throw const FormatException('原串文位置或版本不符');
    }
    final media = (value['media'] as List)
        .map((raw) {
          final image = humanControlObject(raw, {'mediaId', 'mediaVersion'});
          if (!humanControlID(image['mediaId']) ||
              !humanControlVersion(image['mediaVersion'], zero: false)) {
            throw const FormatException('原串文材料不符');
          }
          return Map<String, dynamic>.unmodifiable(image);
        })
        .toList(growable: false);
    return MomentThreadMember(
      value['momentId'],
      value['status'],
      index,
      value['appliedRevision'],
      value['mediaSetVersion'],
      List.unmodifiable(media),
    );
  }
}

class MomentThreadReceipt {
  const MomentThreadReceipt(
    this.operationID,
    this.status,
    this.action,
    this.threadID,
    this.rootMomentID,
    this.expectedVersion,
    this.appliedVersion,
    this.digest,
    this.members,
  );
  final String operationID, status, action, threadID, rootMomentID, digest;
  final int expectedVersion, appliedVersion;
  final List<MomentThreadMember> members;
  factory MomentThreadReceipt.decode(
    Object? raw,
    String operationID, {
    String? expectedDigest,
    String? expectedAction,
  }) {
    final value = humanControlObject(raw, {
      'schemaVersion',
      'operationId',
      'status',
      'action',
      'threadId',
      'rootMomentId',
      'audience',
      'expectedVersion',
      'appliedVersion',
      'payloadDigest',
      'members',
      'committedAt',
    });
    if (value['schemaVersion'] != 'moment-thread-operation-v1' ||
        value['operationId'] != operationID ||
        !humanControlID(operationID) ||
        value['members'] is! List) {
      throw const FormatException('原串文操作收据不符');
    }
    if (value['status'] == 'STOPPED') {
      if (value['action'] != '' ||
          value['threadId'] != '' ||
          value['rootMomentId'] != '' ||
          value['audience'] != '' ||
          value['expectedVersion'] != 0 ||
          value['appliedVersion'] != 0 ||
          value['payloadDigest'] != '' ||
          (value['members'] as List).isNotEmpty ||
          value['committedAt'] != null) {
        throw const FormatException('停止操作收据不符');
      }
      return MomentThreadReceipt(
        operationID,
        'STOPPED',
        '',
        '',
        '',
        0,
        0,
        '',
        const [],
      );
    }
    if (value['status'] != 'SUCCEEDED' ||
        !humanControlID(value['threadId']) ||
        !humanControlID(value['rootMomentId']) ||
        value['audience'] != 'PUBLIC' ||
        !humanControlHash(value['payloadDigest']) ||
        expectedDigest != null && value['payloadDigest'] != expectedDigest ||
        expectedAction != null && value['action'] != expectedAction ||
        !humanControlVersion(value['expectedVersion']) ||
        value['appliedVersion'] != value['expectedVersion'] + 1 ||
        (value['members'] as List).length < 2 ||
        (value['members'] as List).length > 20 ||
        !const {'CREATE_PUBLISH', 'WITHDRAW'}.contains(value['action'])) {
      throw const FormatException('原串文结果不符');
    }
    humanControlTime(value['committedAt']);
    final members = <MomentThreadMember>[];
    for (var i = 0; i < (value['members'] as List).length; i++) {
      members.add(MomentThreadMember.decode(value['members'][i], i));
    }
    if (members.first.momentID != value['rootMomentId'] ||
        members.map((entry) => entry.momentID).toSet().length !=
            members.length ||
        value['action'] == 'CREATE_PUBLISH' &&
            (value['expectedVersion'] != 0 ||
                members.any(
                  (entry) =>
                      entry.status != 'published' ||
                      entry.revision != 2 ||
                      entry.media.isEmpty && entry.mediaSetVersion != 0 ||
                      entry.media.isNotEmpty && entry.mediaSetVersion != 1,
                )) ||
        value['action'] == 'WITHDRAW' &&
            (value['expectedVersion'] < 1 ||
                members.any((entry) => entry.status != 'withdrawn'))) {
      throw const FormatException('原串文成员不符');
    }
    return MomentThreadReceipt(
      operationID,
      value['status'],
      value['action'],
      value['threadId'],
      value['rootMomentId'],
      value['expectedVersion'],
      value['appliedVersion'],
      value['payloadDigest'],
      List.unmodifiable(members),
    );
  }
}

class MomentThreadPreview {
  const MomentThreadPreview(
    this.input,
    this.sourceVersion,
    this.approval,
    this.expiresAt,
    this.members,
    this.materials,
    this.contexts,
  );
  final MomentThreadInput input;
  final String sourceVersion, approval;
  final DateTime expiresAt;
  final List<MomentThreadMember> members;
  final List<List<Map<String, dynamic>>> materials;
  final List<Map<String, dynamic>> contexts;
  bool get current => expiresAt.isAfter(DateTime.now().toUtc());
  factory MomentThreadPreview.decode(Object? raw, MomentThreadInput input) {
    final value = humanControlObject(raw, {
      'operationId',
      'action',
      'payloadDigest',
      'observedAt',
      'expiresAt',
      'approval',
      'input',
      'sourceVersion',
      'members',
      'materials',
      'contexts',
    });
    final echoed = humanControlObject(value['input'], {
      'operationId',
      'action',
      'threadId',
      'localScope',
      'audience',
      'expectedVersion',
      'items',
    });
    if (value['operationId'] != input.operationID ||
        value['action'] != input.action ||
        value['payloadDigest'] != input.digest ||
        momentHash(echoed) != momentHash(input.toJson()) ||
        !humanControlHash(value['sourceVersion']) ||
        value['approval'] is! String ||
        (value['approval'] as String).isEmpty ||
        (value['approval'] as String).length > 8192 ||
        value['members'] is! List ||
        value['materials'] is! List ||
        value['contexts'] is! List) {
      throw const FormatException('整串具体批准版本不符');
    }
    final end = humanControlTime(value['expiresAt']);
    humanControlLease(humanControlTime(value['observedAt']), end);
    final members = <MomentThreadMember>[];
    for (var i = 0; i < (value['members'] as List).length; i++) {
      members.add(MomentThreadMember.decode(value['members'][i], i));
    }
    if (input.action == 'CREATE_PUBLISH'
        ? members.isNotEmpty ||
              (value['materials'] as List).length != input.entries.length
        : members.length < 2 || (value['materials'] as List).isNotEmpty) {
      throw const FormatException('整串预览范围不符');
    }
    final materials = <List<Map<String, dynamic>>>[];
    final contexts = <Map<String, dynamic>>[];
    if ((value['contexts'] as List).length != input.entries.length) {
      throw const FormatException('串文地点预览范围不符');
    }
    for (var i = 0; i < input.entries.length; i++) {
      final place = humanControlObject(value['contexts'][i], {
        'cityId',
        'cityName',
        'placeId',
        'placeName',
      });
      if (place['cityId'] != input.entries[i].cityID ||
          place['placeId'] != input.entries[i].placeID ||
          place['cityName'] is! String ||
          (input.entries[i].cityID.isEmpty
              ? place['cityName'] != ''
              : (place['cityName'] as String).isEmpty) ||
          utf8.encode(place['cityName']).length > 640 ||
          place['placeName'] is! String ||
          utf8.encode(place['placeName']).length > 640 ||
          input.entries[i].placeID.isEmpty && place['placeName'] != '' ||
          input.entries[i].placeID.isNotEmpty &&
              (place['placeName'] as String).isEmpty) {
        throw const FormatException('串文城市或地点名称不符');
      }
      contexts.add(Map.unmodifiable(place));
    }
    for (var i = 0; i < (value['materials'] as List).length; i++) {
      final group = value['materials'][i];
      if (group is! List || group.length != input.entries[i].media.length) {
        throw const FormatException('串文材料数量不符');
      }
      final images = <Map<String, dynamic>>[];
      for (var j = 0; j < group.length; j++) {
        final image = humanControlObject(group[j], {
          'material',
          'thumbnailBase64',
        });
        final stage = PublicMomentMediaStage.decode(image['material']);
        final selected = input.entries[i].media[j];
        if (stage.mediaID != selected.mediaID ||
            stage.version != selected.version ||
            stage.localScope != input.localScope ||
            !stage.current ||
            image['thumbnailBase64'] is! String) {
          throw const FormatException('串文图片原材料已变化');
        }
        final pixels = base64Decode(image['thumbnailBase64']);
        momentMediaJPEG(
          pixels,
          stage.thumbnailBytes,
          stage.thumbnailSHA,
          320,
          180,
        );
        images.add(Map.unmodifiable(image));
      }
      materials.add(List.unmodifiable(images));
    }
    return MomentThreadPreview(
      input,
      value['sourceVersion'],
      value['approval'],
      end,
      List.unmodifiable(members),
      List.unmodifiable(materials),
      List.unmodifiable(contexts),
    );
  }
}

class MomentThreadView {
  const MomentThreadView(
    this.threadID,
    this.rootMomentID,
    this.version,
    this.members,
    this.canWithdraw,
    this.sourceVersion,
    this.validUntil,
  );
  final String threadID, sourceVersion;
  final String? rootMomentID;
  final int version;
  final List<Map<String, dynamic>> members;
  final bool canWithdraw;
  final DateTime validUntil;
  bool get current => validUntil.isAfter(DateTime.now().toUtc());
  factory MomentThreadView.decode(Object? raw) {
    final value = humanControlObject(raw, {
      'schemaVersion',
      'threadId',
      'rootMomentId',
      'audience',
      'version',
      'members',
      'canWithdraw',
      'sourceVersion',
      'observedAt',
      'validUntil',
    });
    if (value['schemaVersion'] != 'moment-thread-view-v1' ||
        !humanControlID(value['threadId']) ||
        value['rootMomentId'] != null &&
            !humanControlID(value['rootMomentId']) ||
        value['audience'] != 'PUBLIC' ||
        !humanControlVersion(value['version'], zero: false) ||
        value['members'] is! List ||
        (value['members'] as List).length < 2 ||
        (value['members'] as List).length > 20 ||
        value['canWithdraw'] is! bool ||
        !humanControlHash(value['sourceVersion'])) {
      throw const FormatException('当前串文资料不符');
    }
    final until = humanControlTime(value['validUntil']);
    humanControlLease(humanControlTime(value['observedAt']), until);
    final members = <Map<String, dynamic>>[];
    final ids = <String>{};
    for (var i = 0; i < (value['members'] as List).length; i++) {
      final member = humanControlObject(value['members'][i], {
        'position',
        'momentId',
        'revision',
        'status',
      });
      if (member['position'] != i ||
          (member['status'] == 'AVAILABLE'
              ? !humanControlID(member['momentId']) ||
                    !humanControlVersion(member['revision'], zero: false) ||
                    member['revision'] < 2 ||
                    !ids.add(member['momentId'])
              : member['status'] != 'UNAVAILABLE' ||
                    member['momentId'] != null ||
                    member['revision'] != 0)) {
        throw const FormatException('当前串文位置不符');
      }
      members.add(Map.unmodifiable(member));
    }
    if (value['rootMomentId'] != members.first['momentId']) {
      throw const FormatException('当前串文根位置不符');
    }
    return MomentThreadView(
      value['threadId'],
      value['rootMomentId'],
      value['version'],
      List.unmodifiable(members),
      value['canWithdraw'],
      value['sourceVersion'],
      until,
    );
  }
}

class MomentThreadHTTPFailure implements Exception {
  const MomentThreadHTTPFailure(this.status);
  final int status;
}

class MomentThreadAPI {
  const MomentThreadAPI({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
    required this.current,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  final bool Function() current;
  String get origin => Uri.parse(baseUrl).origin;
  Future<Object?> request(
    String method,
    String path, {
    Map<String, Object?>? body,
    bool private = true,
  }) async {
    final token = authorizationHeader();
    if (!current() || private && token == null) {
      throw StateError('请使用当前个人身份');
    }
    final request = http.Request(
      method,
      Uri.parse('${baseUrl.replaceFirst(RegExp(r'/$'), '')}$path'),
    );
    request.headers.addAll({
      'Authorization': ?token,
      if (body != null) 'Content-Type': 'application/json',
    });
    if (body != null) {
      request.body = jsonEncode(body);
    }
    final response = await http.Response.fromStream(
      await client.send(request),
    ).timeout(const Duration(seconds: 12));
    if (!current() || token != authorizationHeader()) {
      throw StateError('身份或连接已变化');
    }
    if (response.statusCode != 200) {
      throw MomentThreadHTTPFailure(response.statusCode);
    }
    if (response.bodyBytes.length > 65 * 1024 * 1024) {
      throw const FormatException('串文响应过大');
    }
    return humanControlObject(jsonDecode(utf8.decode(response.bodyBytes)), {
      'data',
    })['data'];
  }

  Future<MomentThreadPreview> preview(MomentThreadInput input) async =>
      MomentThreadPreview.decode(
        await request(
          'POST',
          '/v1/me/moment-threads/previews',
          body: input.toJson(),
        ),
        input,
      );
  Future<MomentThreadReceipt> commit(MomentThreadPreview preview) async =>
      MomentThreadReceipt.decode(
        await request(
          'PUT',
          '/v1/me/moment-threads/operations/${preview.input.operationID}',
          body: {
            ...preview.input.toJson(),
            'sourceVersion': preview.sourceVersion,
            'approval': preview.approval,
          },
        ),
        preview.input.operationID,
        expectedDigest: preview.input.digest,
        expectedAction: preview.input.action,
      );
  Future<MomentThreadReceipt> recover(
    String operationID, {
    bool stop = false,
    String? digest,
    String? action,
  }) async {
    if (!humanControlID(operationID)) {
      throw const FormatException('原串文操作标识不符');
    }
    return MomentThreadReceipt.decode(
      await request(
        stop ? 'POST' : 'GET',
        '/v1/me/moment-threads/operations/$operationID${stop ? '/stop' : ''}',
      ),
      operationID,
      expectedDigest: digest,
      expectedAction: action,
    );
  }

  Future<MomentThreadView> view(String id, {bool byMoment = true}) async {
    if (!humanControlID(id)) {
      throw const FormatException('串文目标不符');
    }
    final value = MomentThreadView.decode(
      await request(
        'GET',
        byMoment ? '/v1/moments/$id/thread' : '/v1/moment-threads/$id',
        private: false,
      ),
    );
    if (byMoment
        ? !value.members.any((entry) => entry['momentId'] == id)
        : value.threadID != id) {
      throw const FormatException('串文查询目标不符');
    }
    return value;
  }
}
