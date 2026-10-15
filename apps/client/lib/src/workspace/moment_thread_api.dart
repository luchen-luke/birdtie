import 'dart:convert';
import 'package:http/http.dart' as http;
import '../content/moment_control.dart';
import 'human_causal_control.dart';
import 'public_moment_media.dart';
import 'entity_cover.dart';
import '../content/moment_locations.dart';
import '../content/private_moment_controller.dart';

/// EDIT alone can preserve original associations without a location. Validate
/// the ordinary fields through the existing contract, then bind the separately
/// validated association selectors into the same canonical input tuple.
String _editMomentInputHash(Map<String, Object?> input, String localID) {
  const contextKeys = ['activityId', 'communityId', 'organizationId'];
  final ordinary = Map<String, Object?>.from(input);
  for (final key in contextKeys) {
    final value = ordinary.remove(key);
    if (input.containsKey(key) &&
        (value is! String || value.isNotEmpty && !humanControlID(value))) {
      throw const FormatException('原动态关联无效');
    }
  }
  MomentControlInput(
    operationID: localID,
    action: 'UPDATE',
    momentID: localID,
    expectedRevision: 1,
    input: ordinary,
  );
  String encoded(String key) => base64Encode(utf8.encode(input[key] as String));
  final locations = input.containsKey('locations')
      ? decodeMomentLocationInputs(input['locations'])
      : null;
  return momentHash([
    'moment-input-v1',
    encoded('cityId'),
    input['placeId'],
    encoded('title'),
    encoded('body'),
    input['occurredAt'] == null
        ? ''
        : momentUTC(humanControlTime(input['occurredAt'])),
    input['timePrecision'],
    input['locationPrecision'],
    for (final key in contextKeys) input[key],
    if (locations != null) ...[
      locations.any((v) => v.kind == 'REFERENCE')
          ? 'moment-locations-v2'
          : 'moment-locations-v1',
      locations.map((v) => v.digestTuple).toList(),
    ],
  ]);
}

/// Local recovery selectors only. Pixels, credentials and use-rights approval
/// are kept out of the text journal. An unresolved selector blocks publication.
class MomentDraftMaterialReference {
  const MomentDraftMaterialReference(this.operationID, this.kind);
  final String operationID, kind;
  Map<String, Object> get json => {'operationId': operationID, 'kind': kind};
  factory MomentDraftMaterialReference.decode(Object? raw) {
    final value = humanControlObject(raw, {'operationId', 'kind'});
    if (!humanControlID(value['operationId']) ||
        !const {'IMAGE', 'VIDEO'}.contains(value['kind'])) {
      throw const FormatException('原媒体恢复记录无效');
    }
    return MomentDraftMaterialReference(value['operationId'], value['kind']);
  }
}

class MomentThreadDraftEntry {
  MomentThreadDraftEntry({
    required this.localID,
    required this.cityID,
    this.placeID = '',
    this.text = '',
    String? title,
    this.mediaSummary = '',
    List<PublicMomentMediaSelection> media = const [],
    List<MomentLocationInput>? locations,
    this.momentID = '',
    this.expectedRevision = 0,
    this.originalInput,
    this.materialJournalEntryID,
    this.originalStatus = 'draft',
    this.preserveMedia = false,
    this.restore = false,
    List<MomentDraftMaterialReference> materialReferences = const [],
  }) : media = List.unmodifiable(media),
       title = title ?? originalInput?['title'] as String? ?? '',
       materialReferences = List.unmodifiable(materialReferences),
       locations = locations == null ? null : List.unmodifiable(locations);
  final String localID, cityID, placeID, text;
  final String title, mediaSummary;
  final String momentID;
  final int expectedRevision;
  final Map<String, dynamic>? originalInput;
  final String? materialJournalEntryID;
  String get journalEntryID => materialJournalEntryID ?? localID;
  final String originalStatus;
  final bool preserveMedia;
  final bool restore;
  final List<MomentDraftMaterialReference> materialReferences;
  bool get hasConflictingMaterialReferences =>
      materialReferences.length > publicMomentMediaMaxImages ||
      materialReferences.any((v) => v.kind == 'VIDEO') &&
          materialReferences.length != 1;
  final List<PublicMomentMediaSelection> media;
  final List<MomentLocationInput>? locations;
  factory MomentThreadDraftEntry.owned(
    PrivateMoment moment, {
    List<MomentLocationInput>? locations,
  }) => MomentThreadDraftEntry(
    localID: moment.id,
    momentID: moment.id,
    expectedRevision: moment.revision,
    cityID: locations == null ? moment.cityID : '',
    placeID: locations == null ? moment.placeID : '',
    text: moment.body,
    locations: locations,
    originalInput: Map.unmodifiable({
      'title': moment.title,
      'body': moment.body,
      'cityId': moment.cityID,
      'placeId': moment.placeID,
      'occurredAt': moment.occurredAt?.toUtc().toIso8601String(),
      'timePrecision': moment.timePrecision,
      'locationPrecision': moment.locationPrecision,
      if (moment.activityIDs.isNotEmpty)
        'activityId': moment.activityIDs.single,
      if (moment.communityID.isNotEmpty) 'communityId': moment.communityID,
      if (moment.organizationID.isNotEmpty)
        'organizationId': moment.organizationID,
    }),
    originalStatus: moment.status,
    preserveMedia: moment.status == 'published',
  );
  MomentThreadDraftEntry copy({
    String? cityID,
    String? placeID,
    String? text,
    String? title,
    String? mediaSummary,
    List<PublicMomentMediaSelection>? media,
    List<MomentLocationInput>? locations,
    int? expectedRevision,
    String? momentID,
    String? originalStatus,
    bool? preserveMedia,
    bool? restore,
    String? materialJournalEntryID,
    List<MomentDraftMaterialReference>? materialReferences,
  }) => MomentThreadDraftEntry(
    localID: localID,
    cityID: cityID ?? this.cityID,
    placeID: placeID ?? this.placeID,
    text: text ?? this.text,
    title: title ?? this.title,
    mediaSummary: mediaSummary ?? this.mediaSummary,
    media: media ?? this.media,
    locations: locations ?? this.locations,
    momentID: momentID ?? this.momentID,
    expectedRevision: expectedRevision ?? this.expectedRevision,
    originalInput: originalInput,
    materialJournalEntryID:
        materialJournalEntryID ?? this.materialJournalEntryID,
    originalStatus: originalStatus ?? this.originalStatus,
    preserveMedia: preserveMedia ?? this.preserveMedia,
    restore: restore ?? this.restore,
    materialReferences: materialReferences ?? this.materialReferences,
  );
  Map<String, Object?> get input => {
    'cityId': locations == null ? cityID : '',
    'placeId': locations == null ? placeID : '',
    'occurredAt': null,
    'timePrecision': 'unknown',
    'locationPrecision': locations != null
        ? 'none'
        : placeID.isNotEmpty
        ? 'place'
        : (cityID.isEmpty ? 'none' : 'city'),
    if (originalInput != null) ...originalInput!,
    'title': title.trim(),
    'body': text.trim(),
    if (locations != null) ...{
      'cityId': '',
      'placeId': '',
      'locationPrecision': 'none',
    },
    if (locations != null)
      'locations': locations!.map((v) => v.toJson()).toList(),
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

  String get editInputHash => _editMomentInputHash(input, localID);

  Map<String, Object?> toItem({String localScope = '', bool existing = false}) {
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
      if (existing) ...{
        'momentId': momentID,
        'expectedRevision': expectedRevision,
      },
      'input': input,
      'media': media.map((image) => image.toJson()).toList(),
    };
  }

  Map<String, Object?> toEditItem(String localScope) {
    if (originalStatus == 'withdrawn' && !restore) {
      throw const FormatException('这条内容已撤回，请明确选择重新发布');
    }
    if (momentID.isEmpty
        ? expectedRevision != 0 || restore || preserveMedia
        : !humanControlID(momentID) ||
              !humanControlVersion(expectedRevision, zero: false)) {
      throw const FormatException('原动态版本无效');
    }
    if (text.trim().isEmpty && media.isEmpty && !preserveMedia ||
        hasConflictingMaterialReferences ||
        materialReferences.length > media.length ||
        media.length > 9 ||
        media.any(
          (v) =>
              !humanControlID(v.mediaID) ||
              !humanControlVersion(v.version, zero: false) ||
              !v.rights.valid,
        )) {
      throw const FormatException('请补充正文或准备好全部媒体；未完成材料不能忽略');
    }
    editInputHash;
    return {
      'momentId': momentID,
      'expectedRevision': expectedRevision,
      'restore': restore,
      'input': input,
      'media': preserveMedia ? null : media.map((v) => v.toJson()).toList(),
    };
  }

  Map<String, Object?> get json => {
    'localId': localID,
    'cityId': cityID,
    'placeId': placeID,
    'text': text,
    'title': title,
    if (mediaSummary.isNotEmpty) 'mediaSummary': mediaSummary,
    'media': media.map((image) => image.toJson()).toList(),
    if (materialJournalEntryID != null)
      'materialJournalEntryId': materialJournalEntryID,
    if (originalStatus != 'draft') 'originalStatus': originalStatus,
    if (preserveMedia) 'preserveMedia': true,
    if (restore) 'restore': true,
    if (materialReferences.isNotEmpty)
      'materialReferences': materialReferences.map((v) => v.json).toList(),
    if (locations != null)
      'locations': locations!.map((v) => v.toJson()).toList(),
    if (momentID.isNotEmpty) ...{
      'momentId': momentID,
      'expectedRevision': expectedRevision,
      'originalInput': originalInput == null
          ? null
          : (Map<String, dynamic>.from(originalInput!)..remove('body')),
    },
  };
  factory MomentThreadDraftEntry.decode(Object? raw) {
    final value = humanControlObject(
      raw,
      {'localId', 'cityId', 'placeId', 'text', 'media'},
      optional: {
        'locations',
        'momentId',
        'expectedRevision',
        'originalInput',
        'materialJournalEntryId',
        'originalStatus',
        'preserveMedia',
        'materialReferences',
        'title',
        'mediaSummary',
        'restore',
      },
    );
    if (!humanControlID(value['localId']) ||
        value['cityId'] is! String ||
        utf8.encode(value['cityId']).length > 80 ||
        value['placeId'] is! String ||
        value['placeId'] != '' && !humanControlID(value['placeId']) ||
        value['text'] is! String ||
        utf8.encode(value['text']).length > 6000 ||
        value.containsKey('title') &&
            (value['title'] is! String ||
                utf8.encode(value['title']).length > 160) ||
        value.containsKey('mediaSummary') &&
            (value['mediaSummary'] is! String ||
                utf8.encode(value['mediaSummary']).length > 256) ||
        value['media'] is! List ||
        (value['media'] as List).length > 9) {
      throw const FormatException('串文私人草稿不符');
    }
    final journalEntry = value['materialJournalEntryId'];
    final originalStatus = value['originalStatus'] ?? 'draft';
    if (journalEntry != null &&
            journalEntry != '' &&
            !humanControlID(journalEntry) ||
        !const {'draft', 'published', 'withdrawn'}.contains(originalStatus) ||
        value.containsKey('preserveMedia') && value['preserveMedia'] is! bool ||
        value.containsKey('restore') && value['restore'] is! bool) {
      throw const FormatException('动态私人恢复归属无效');
    }
    final refs = value['materialReferences'] ?? const [];
    // Older versions could journal the original selectors alongside a complete
    // replacement group. Read that bounded failed state without losing text,
    // locations or stable IDs; it is never an eligible publication bundle.
    if (refs is! List || refs.length > publicMomentMediaMaxImages * 2) {
      throw const FormatException('动态媒体恢复记录过多');
    }
    final references = refs.map(MomentDraftMaterialReference.decode).toList();
    if (references.map((v) => v.operationID).toSet().length !=
        references.length) {
      throw const FormatException('动态媒体恢复顺序无效');
    }
    return MomentThreadDraftEntry(
      localID: value['localId'],
      cityID: value['cityId'],
      placeID: value['placeId'],
      text: value['text'],
      title: value.containsKey('title') ? value['title'] as String : null,
      mediaSummary: value['mediaSummary'] as String? ?? '',
      momentID: value['momentId'] as String? ?? '',
      expectedRevision: value['expectedRevision'] as int? ?? 0,
      originalInput: value['originalInput'] == null
          ? null
          : Map<String, dynamic>.from(value['originalInput']),
      materialJournalEntryID: journalEntry as String?,
      originalStatus: originalStatus as String,
      preserveMedia: value['preserveMedia'] == true,
      restore: value['restore'] == true,
      materialReferences: references,
      locations: value.containsKey('locations')
          ? decodeMomentLocationInputs(value['locations'])
          : null,
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
    this.baseMomentID = '',
    List<Map<String, Object>> removedItems = const [],
    List<MomentThreadDraftEntry> entries = const [],
  }) : entries = List.unmodifiable(entries),
       removedItems = List.unmodifiable(
         removedItems.map((v) => Map<String, Object>.unmodifiable(v)),
       );
  final String operationID, action, threadID, localScope;
  final int expectedVersion;
  final String baseMomentID;
  final List<Map<String, Object>> removedItems;
  final List<MomentThreadDraftEntry> entries;
  void validate() {
    if (!humanControlID(operationID) ||
        !humanControlVersion(expectedVersion) ||
        expectedVersion >= humanControlMaxVersion) {
      throw const FormatException('串文操作不符');
    }
    if (action == 'EDIT') {
      if (!humanControlID(localScope) ||
          entries.isEmpty ||
          entries.length > 20 ||
          (threadID.isEmpty
              ? expectedVersion != 0
              : !humanControlID(threadID) ||
                    expectedVersion < 1 ||
                    baseMomentID.isNotEmpty) ||
          baseMomentID.isNotEmpty &&
              (!humanControlID(baseMomentID) ||
                  !entries.any((v) => v.momentID == baseMomentID))) {
        throw const FormatException('动态编辑目标无效');
      }
      final ids = <String>{}, materials = <String>{};
      for (final entry in entries) {
        entry.toEditItem(localScope);
        if (!ids.add(entry.momentID.isEmpty ? entry.localID : entry.momentID)) {
          throw const FormatException('同一动态不能重复放入');
        }
        for (final image in entry.media) {
          if (!materials.add(image.mediaID))
            throw const FormatException('同一材料不能分给多条');
        }
      }
      for (final removed in removedItems) {
        if (removed.length != 2 ||
            !humanControlID(removed['momentId']) ||
            !humanControlVersion(removed['expectedRevision'], zero: false) ||
            !ids.add(removed['momentId'] as String)) {
          throw const FormatException('待移除的原动态版本无效');
        }
      }
    } else if (action == 'CREATE_PUBLISH' || action == 'PUBLISH') {
      if ((action == 'CREATE_PUBLISH'
              ? threadID.isNotEmpty || expectedVersion != 0
              : !humanControlID(threadID) || expectedVersion < 1) ||
          !humanControlID(localScope) ||
          entries.length < 2 ||
          entries.length > 20) {
        throw const FormatException('请保留2至20条有序内容');
      }
      final seen = <String>{};
      final memberIDs = <String>{};
      for (final entry in entries) {
        if (action == 'PUBLISH' &&
            (!humanControlID(entry.momentID) ||
                !memberIDs.add(entry.momentID) ||
                !humanControlVersion(entry.expectedRevision, zero: false))) {
          throw const FormatException('原串文动态版本不符');
        }
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
      if (action == 'EDIT') ...{
        'baseMomentId': baseMomentID,
        'removedItems': removedItems,
      },
      'items': entries
          .map(
            (entry) => action == 'EDIT'
                ? entry.toEditItem(localScope)
                : entry.toItem(
                    localScope: localScope,
                    existing: action == 'PUBLISH',
                  ),
          )
          .toList(),
    };
  }

  String get digest {
    validate();
    if (action == 'EDIT') {
      return momentHash([
        'moment-thread-edit-v1',
        operationID,
        action,
        threadID,
        baseMomentID,
        localScope,
        'PUBLIC',
        expectedVersion,
        entries
            .map(
              (entry) => [
                entry.momentID,
                entry.expectedRevision,
                entry.restore,
                entry.editInputHash,
                entry.preserveMedia
                    ? null
                    : entry.media
                          .map((v) => [v.mediaID, v.version, v.rights.digest])
                          .toList(),
              ],
            )
            .toList(),
        removedItems
            .map((v) => [v['momentId'], v['expectedRevision']])
            .toList(),
      ]);
    }
    return momentHash([
      action == 'PUBLISH' ? 'moment-thread-existing-v2' : 'moment-thread-v1',
      operationID,
      action,
      threadID,
      localScope,
      'PUBLIC',
      expectedVersion,
      entries
          .map(
            (entry) => [
              if (action == 'PUBLISH') ...[
                entry.momentID,
                entry.expectedRevision,
              ],
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

bool _threadPreviewEchoMatches(
  Map<String, dynamic> echoed,
  MomentThreadInput expected,
) {
  final requested = expected.toJson();
  for (final key in const [
    'operationId',
    'action',
    'threadId',
    'localScope',
    'audience',
    'expectedVersion',
  ]) {
    if (echoed[key] != requested[key]) return false;
  }
  if (expected.action == 'EDIT') {
    final removed = echoed['removedItems'];
    if (echoed['baseMomentId'] != expected.baseMomentID ||
        removed is! List ||
        removed.length != expected.removedItems.length) {
      return false;
    }
    for (var i = 0; i < removed.length; i++) {
      final actual = humanControlObject(removed[i], {
        'momentId',
        'expectedRevision',
      });
      final selected = expected.removedItems[i];
      if (actual['momentId'] != selected['momentId'] ||
          actual['expectedRevision'] != selected['expectedRevision']) {
        return false;
      }
    }
  }
  final rows = echoed['items'];
  if (rows is! List || rows.length != expected.entries.length) return false;
  for (var i = 0; i < rows.length; i++) {
    final entry = expected.entries[i];
    final item = humanControlObject(rows[i], {
      'input',
      'media',
      if (expected.action == 'PUBLISH' || expected.action == 'EDIT') ...{
        'momentId',
        'expectedRevision',
      },
      if (expected.action == 'EDIT') 'restore',
    });
    if ((expected.action == 'PUBLISH' || expected.action == 'EDIT') &&
        (item['momentId'] != entry.momentID ||
            item['expectedRevision'] != entry.expectedRevision)) {
      return false;
    }
    if (expected.action == 'EDIT' &&
        (item['restore'] != entry.restore ||
            (item['media'] == null) != entry.preserveMedia))
      return false;
    final input = humanControlObject(
      item['input'],
      {
        'cityId',
        'placeId',
        'title',
        'body',
        'timePrecision',
        'locationPrecision',
      },
      optional: {
        'occurredAt',
        'locations',
        if (expected.action == 'EDIT') ...{
          'activityId',
          'communityId',
          'organizationId',
        },
      },
    );
    // Decode the same closed material selectors as the local draft. JSON object
    // order is not part of the contract; entry and material order still are.
    final materialEntry = MomentThreadDraftEntry.decode({
      'localId': entry.localID,
      'cityId': input['cityId'],
      'placeId': input['placeId'],
      'text': input['body'],
      'media': item['media'] ?? const [],
    });
    final actualInputHash = expected.action == 'EDIT'
        ? _editMomentInputHash(input.cast<String, Object?>(), entry.localID)
        : MomentControlInput(
            operationID: entry.localID,
            action: entry.preserveMedia ? 'UPDATE_PUBLIC' : 'PUBLISH',
            momentID: entry.localID,
            expectedRevision: 1,
            input: input.cast<String, Object?>(),
            media: expected.action == 'EDIT' || materialEntry.media.isEmpty
                ? null
                : materialEntry.media,
            localScope: expected.action == 'EDIT' || materialEntry.media.isEmpty
                ? ''
                : expected.localScope,
          ).inputHash;
    if (actualInputHash !=
            (expected.action == 'EDIT'
                ? entry.editInputHash
                : entry._controlInput(expected.localScope).inputHash) ||
        materialEntry.media.length != entry.media.length) {
      return false;
    }
    for (var j = 0; j < entry.media.length; j++) {
      final actual = materialEntry.media[j], selected = entry.media[j];
      if (actual.mediaID != selected.mediaID ||
          actual.version != selected.version ||
          actual.rights.digest != selected.rights.digest) {
        return false;
      }
    }
  }
  return true;
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
    this.members, {
    this.removedMembers = const [],
  });
  final String operationID, status, action, threadID, rootMomentID, digest;
  final int expectedVersion, appliedVersion;
  final List<MomentThreadMember> members;
  final List<MomentThreadMember> removedMembers;
  factory MomentThreadReceipt.decode(
    Object? raw,
    String operationID, {
    String? expectedDigest,
    String? expectedAction,
  }) {
    final typed =
        raw is Map<String, dynamic> &&
        raw['schemaVersion'] == 'moment-thread-operation-v2';
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
      if (typed) 'removedMembers',
    });
    if (value['schemaVersion'] !=
            (typed
                ? 'moment-thread-operation-v2'
                : 'moment-thread-operation-v1') ||
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
        (value['members'] as List).length < (typed ? 1 : 2) ||
        (value['members'] as List).length > 20 ||
        !{
          'CREATE_PUBLISH',
          'PUBLISH',
          'WITHDRAW',
          if (typed) 'EDIT',
        }.contains(value['action'])) {
      throw const FormatException('原串文结果不符');
    }
    humanControlTime(value['committedAt']);
    final members = <MomentThreadMember>[];
    for (var i = 0; i < (value['members'] as List).length; i++) {
      members.add(MomentThreadMember.decode(value['members'][i], i));
    }
    if (!typed && members.first.momentID != value['rootMomentId'] ||
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
        value['action'] == 'EDIT' &&
            (!typed || members.any((v) => v.status != 'published')) ||
        value['action'] == 'PUBLISH' &&
            (value['expectedVersion'] < 1 ||
                members.any((entry) => entry.status != 'published')) ||
        value['action'] == 'WITHDRAW' &&
            (value['expectedVersion'] < 1 ||
                members.any((entry) => entry.status != 'withdrawn'))) {
      throw const FormatException('原串文成员不符');
    }
    final removed = <MomentThreadMember>[];
    if (typed) {
      if (value['removedMembers'] is! List ||
          (value['removedMembers'] as List).length > 20) {
        throw const FormatException('原动态删除结果无效');
      }
      final removedIDs = <String>{};
      for (final raw in value['removedMembers'] as List) {
        if (raw is! Map<String, dynamic> ||
            raw['position'] is! int ||
            raw['position'] < 0) {
          throw const FormatException('原动态删除位置无效');
        }
        final member = MomentThreadMember.decode(raw, raw['position']);
        if (member.status != 'withdrawn' ||
            !removedIDs.add(member.momentID) ||
            members.any((v) => v.momentID == member.momentID)) {
          throw const FormatException('原动态删除范围无效');
        }
        removed.add(member);
      }
      if (value['action'] != 'EDIT' && removed.isNotEmpty) {
        throw const FormatException('原动态删除动作无效');
      }
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
      removedMembers: List.unmodifiable(removed),
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
    this.contexts, {
    this.locations = const [],
    this.preservedMedia = const [],
  });
  final MomentThreadInput input;
  final String sourceVersion, approval;
  final DateTime expiresAt;
  final List<MomentThreadMember> members;
  final List<List<Map<String, dynamic>>> materials;
  final List<Map<String, dynamic>> contexts;
  final List<List<MomentLocationResolved>> locations;
  final List<PublicMomentMediaSet?> preservedMedia;
  bool get current =>
      expiresAt.isAfter(DateTime.now().toUtc()) &&
      preservedMedia.every((v) => v == null || v.current) &&
      locations
          .expand((row) => row)
          .every((location) => !location.available || location.current);
  factory MomentThreadPreview.decode(Object? raw, MomentThreadInput input) {
    final value = humanControlObject(
      raw,
      {
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
      },
      optional: {'locations', if (input.action == 'EDIT') 'preservedMedia'},
    );
    final echoed = humanControlObject(value['input'], {
      'operationId',
      'action',
      'threadId',
      'localScope',
      'audience',
      'expectedVersion',
      'items',
      if (input.action == 'EDIT') ...{'baseMomentId', 'removedItems'},
    });
    if (value['operationId'] != input.operationID ||
        value['action'] != input.action ||
        value['payloadDigest'] != input.digest ||
        !_threadPreviewEchoMatches(echoed, input) ||
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
    if (input.action == 'CREATE_PUBLISH' ||
            input.action == 'PUBLISH' ||
            input.action == 'EDIT'
        ? members.isNotEmpty ||
              (value['materials'] as List).length != input.entries.length
        : members.isEmpty || (value['materials'] as List).isNotEmpty) {
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
    final locations = <List<MomentLocationResolved>>[];
    final preserved = <PublicMomentMediaSet?>[];
    if (input.action == 'EDIT') {
      final rows = value['preservedMedia'];
      if (rows is! List || rows.length != input.entries.length) {
        throw const FormatException('保留媒体的批准范围无效');
      }
      for (var i = 0; i < rows.length; i++) {
        final entry = input.entries[i], row = rows[i];
        if (row != null && !entry.preserveMedia)
          throw const FormatException('媒体保留方式已变化');
        preserved.add(
          row == null
              ? null
              : PublicMomentMediaSet.decode(
                  row,
                  entry.momentID,
                  expectedRevision: entry.expectedRevision,
                  own: true,
                ),
        );
      }
    }
    final rawLocations = value['locations'];
    if (rawLocations != null) {
      if (rawLocations is! List || rawLocations.length != input.entries.length)
        throw const FormatException('串文位置集合不符');
      for (var i = 0; i < rawLocations.length; i++) {
        final row = rawLocations[i];
        if (row is! List || row.length > momentLocationMaxLinks)
          throw const FormatException('串文位置集合不符');
        final resolved = <MomentLocationResolved>[];
        for (var j = 0; j < row.length; j++)
          resolved.add(MomentLocationResolved.decode(row[j], j));
        if (resolved.map((v) => v.id).toSet().length != resolved.length)
          throw const FormatException('串文位置重复');
        final requested = input.entries[i].locations;
        if (requested != null &&
            jsonEncode(requested.map((v) => v.toJson()).toList()) !=
                jsonEncode(resolved.map((v) => v.input.toJson()).toList()))
          throw const FormatException('串文位置批准与选择不符');
        locations.add(List.unmodifiable(resolved));
      }
    } else if (input.entries.any((e) => e.locations != null)) {
      throw const FormatException('串文位置批准缺失');
    }
    return MomentThreadPreview(
      input,
      value['sourceVersion'],
      value['approval'],
      end,
      List.unmodifiable(members),
      List.unmodifiable(materials),
      List.unmodifiable(contexts),
      locations: List.unmodifiable(locations),
      preservedMedia: List.unmodifiable(preserved),
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
    final typed =
        raw is Map<String, dynamic> &&
        raw['schemaVersion'] == 'moment-thread-view-v2';
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
    if (value['schemaVersion'] !=
            (typed ? 'moment-thread-view-v2' : 'moment-thread-view-v1') ||
        !humanControlID(value['threadId']) ||
        value['rootMomentId'] != null &&
            !humanControlID(value['rootMomentId']) ||
        value['audience'] != 'PUBLIC' ||
        !humanControlVersion(value['version'], zero: false) ||
        value['members'] is! List ||
        (value['members'] as List).length < (typed ? 1 : 2) ||
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
    if (!typed && value['rootMomentId'] != members.first['momentId']) {
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

class OwnedMomentThread {
  const OwnedMomentThread(
    this.threadID,
    this.rootMomentID,
    this.audience,
    this.status,
    this.version,
    this.members,
    this.entryContexts,
    this.legs,
    this.sourceVersion,
    this.validUntil, {
    this.retiredMembers = const [],
  });
  final String threadID, audience, status, sourceVersion;
  final String? rootMomentID;
  final int version;
  final List<Map<String, dynamic>> members, entryContexts, legs;
  final List<Map<String, dynamic>> retiredMembers;
  final DateTime validUntil;
  bool get current => DateTime.now().toUtc().isBefore(validUntil);
  factory OwnedMomentThread.decode(Object? raw) {
    final typed =
        raw is Map<String, dynamic> &&
        raw['schemaVersion'] == 'owned-moment-thread-v2';
    final v = humanControlObject(raw, {
      'schemaVersion',
      'threadId',
      'rootMomentId',
      'audience',
      'status',
      'version',
      'members',
      'entryContexts',
      'legs',
      'sourceVersion',
      'observedAt',
      'validUntil',
      if (typed) 'retiredMembers',
    });
    if (v['schemaVersion'] !=
            (typed ? 'owned-moment-thread-v2' : 'owned-moment-thread-v1') ||
        !humanControlID(v['threadId']) ||
        v['rootMomentId'] != null && !humanControlID(v['rootMomentId']) ||
        !{'PRIVATE', 'PUBLIC'}.contains(v['audience']) ||
        !{'draft', 'published', 'withdrawn'}.contains(v['status']) ||
        !humanControlVersion(v['version'], zero: false) ||
        !humanControlHash(v['sourceVersion']) ||
        v['members'] is! List ||
        (v['members'] as List).isEmpty ||
        (v['members'] as List).length > 20 ||
        v['entryContexts'] is! List ||
        v['legs'] is! List ||
        (v['legs'] as List).length > 100)
      throw const FormatException('原串文来源不符');
    final until = humanControlTime(v['validUntil']),
        at = humanControlTime(v['observedAt']);
    humanControlLease(at, until);
    if (until.difference(at) > const Duration(seconds: 30))
      throw const FormatException('原串文已变化');
    final members = <Map<String, dynamic>>[],
        contexts = <Map<String, dynamic>>[],
        legs = <Map<String, dynamic>>[],
        ids = <String>{};
    for (final raw in v['members'] as List) {
      final m = humanControlObject(raw, {
        'position',
        'momentId',
        'revision',
        'status',
      });
      if (m['position'] != members.length ||
          (m['status'] == 'AVAILABLE'
              ? !humanControlID(m['momentId']) ||
                    !ids.add(m['momentId']) ||
                    !humanControlVersion(m['revision'], zero: false)
              : m['status'] != 'UNAVAILABLE' ||
                    m['momentId'] != null ||
                    m['revision'] != 0))
        throw const FormatException('原串文成员不符');
      members.add(Map.unmodifiable(m));
    }
    if (!typed && members.first['momentId'] != v['rootMomentId'])
      throw const FormatException('原串文顺序不符');
    final retired = <Map<String, dynamic>>[];
    if (typed) {
      if (v['retiredMembers'] is! List)
        throw const FormatException('原动态移除记录无效');
      for (final raw in v['retiredMembers'] as List) {
        final item = humanControlObject(raw, {
          'position',
          'momentId',
          'revision',
          'status',
        });
        if (item['position'] is! int ||
            item['position'] < 0 ||
            !humanControlID(item['momentId']) ||
            !ids.add(item['momentId']) ||
            !humanControlVersion(item['revision'], zero: false) ||
            item['status'] != 'RETIRED')
          throw const FormatException('原动态移除记录归属无效');
        retired.add(Map.unmodifiable(item));
      }
    }
    for (final raw in v['entryContexts'] as List) {
      final e = humanControlObject(raw, {
        'position',
        'momentId',
        'inboundOrigin',
        'inboundMode',
        'durationMinutes',
        'estimatedDurationMinutes',
      });
      if (e['position'] is! int ||
          e['position'] < 0 ||
          (!typed &&
              (e['position'] >= members.length ||
                  members[e['position']]['momentId'] != e['momentId'])) ||
          typed && !ids.contains(e['momentId']) ||
          e['inboundOrigin'] is! String ||
          e['inboundMode'] is! String ||
          (e['inboundOrigin'] as String).length > 200 ||
          (e['inboundMode'] as String).length > 80 ||
          [
            e['durationMinutes'],
            e['estimatedDurationMinutes'],
          ].any((n) => n != null && (n is! int || n < 0 || n > 525600)))
        throw const FormatException('原串文行程信息不符');
      contexts.add(Map.unmodifiable(e));
    }
    for (final raw in v['legs'] as List) {
      final l = humanControlObject(raw, {
        'id',
        'fromMomentId',
        'toMomentId',
        'fromPosition',
        'toPosition',
        'mode',
        'fromPlaceId',
        'toPlaceId',
        'sourceCreatedAt',
        'sourceUpdatedAt',
      });
      if (!humanControlID(l['id']) ||
          !ids.contains(l['fromMomentId']) ||
          !ids.contains(l['toMomentId']) ||
          l['fromPosition'] is! int ||
          l['toPosition'] is! int ||
          l['fromPosition'] < 0 ||
          l['toPosition'] < 0 ||
          (!typed &&
              (l['fromPosition'] >= members.length ||
                  l['toPosition'] >= members.length ||
                  members[l['fromPosition']]['momentId'] != l['fromMomentId'] ||
                  members[l['toPosition']]['momentId'] != l['toMomentId'])) ||
          l['mode'] is! String ||
          (l['mode'] as String).length > 80 ||
          [
            l['fromPlaceId'],
            l['toPlaceId'],
          ].any((id) => id != '' && !humanControlID(id)))
        throw const FormatException('原串文路线信息不符');
      for (final k in ['sourceCreatedAt', 'sourceUpdatedAt']) {
        if (l[k] != null) humanControlTime(l[k]);
      }
      legs.add(Map.unmodifiable(l));
    }
    return OwnedMomentThread(
      v['threadId'],
      v['rootMomentId'],
      v['audience'],
      v['status'],
      v['version'],
      List.unmodifiable(members),
      List.unmodifiable(contexts),
      List.unmodifiable(legs),
      v['sourceVersion'],
      until,
      retiredMembers: List.unmodifiable(retired),
    );
  }
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

  Future<OwnedMomentThread> owned(String id, {bool byMoment = true}) async {
    if (!humanControlID(id)) throw const FormatException('原串文目标不符');
    final value = OwnedMomentThread.decode(
      await request(
        'GET',
        byMoment ? '/v1/me/moments/$id/thread' : '/v1/me/moment-threads/$id',
      ),
    );
    if (byMoment
        ? !value.members.any((v) => v['momentId'] == id)
        : value.threadID != id)
      throw const FormatException('原串文目标不符');
    return value;
  }
}
