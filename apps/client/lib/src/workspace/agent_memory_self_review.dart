import 'dart:convert';
import '../content/agent_profile_completion_api.dart'
    show completionMap, completionVersion, profileCompletionStatements;
import 'agent_memory_candidate_api.dart'
    show candidateFreezeJson, candidateIDValid;
import 'agent_memory_correction_api.dart';

const selfReviewNotice =
    '仅核对已选择来源中的明确活动类别偏好；相反声明保留双方并等待本人确认。没有列出冲突不代表所有声明都没有冲突。更新时间和读取时间不证明发生、出席、到访或当前位置。';
const selfReviewConflictExplanation = '这些声明不一致，等待本人确认；不会据此改写身份、权限或位置。';
void _check(bool ok) {
  if (!ok) throw const FormatException('同体来源审阅或当前记忆不符');
}

bool _same(dynamic value, DateTime time) => correctionTime(value) == time;

/// An expiring human description, never a correction preview or permission.
class MemorySelfReview {
  MemorySelfReview._(this.raw, this.categories, this.conflictCategories);
  final Map<String, dynamic> raw;
  final List<String> categories, conflictCategories;
  DateTime get observedAt => correctionTime(raw['observedAt']);
  DateTime get expiresAt => correctionTime(raw['expiresAt']);
  String get profileStatus => raw['sections']['profile'];
  Map<String, dynamic> get memory => raw['memories'].single;
  bool get confidenceProvided => memory.containsKey('confidence');
  DateTime? get createdAt => memory.containsKey('createdAt')
      ? correctionTime(memory['createdAt'])
      : null;
  DateTime? get validFrom => memory.containsKey('validFrom')
      ? correctionTime(memory['validFrom'])
      : null;

  factory MemorySelfReview.read(
    dynamic value, {
    required String owner,
    required CorrectableMemory selected,
    required DateTime now,
  }) {
    _check(utf8.encode(jsonEncode({'data': value})).length + 1 <= 64 * 1024);
    final d = completionMap(value, {
      'schemaVersion',
      'owner',
      'agentId',
      'selection',
      'sections',
      'profile',
      'memories',
      'policies',
      'fieldEvidenceSet',
      'observedAt',
      'expiresAt',
      'comparisonScope',
      'notice',
      'modelAccess',
      'mediaAccess',
      'memoryPromotionAllowed',
      'grantsAuthority',
    });
    final o = completionMap(d['owner'], {'type', 'id'});
    final pick = completionMap(d['selection'], {
      'profileFields',
      'memoryIds',
      'policyFamilies',
    });
    final sections = completionMap(d['sections'], {
      'profile',
      'memories',
      'policies',
    });
    final at = correctionTime(d['observedAt']),
        end = correctionTime(d['expiresAt']);
    _check(
      candidateIDValid(owner) &&
          selected.raw['ownerId'] == owner &&
          selected.sourceType == 'EXPLICIT' &&
          selected.status == 'ACTIVE' &&
          selected.validUntil.isAfter(now) &&
          o['type'] == 'PERSON' &&
          o['id'] == owner &&
          d['agentId'] == selected.agentID &&
          d['schemaVersion'] == 'human-self-review-field-evidence-v1' &&
          jsonEncode(pick['profileFields']) == '["preferredActivityTypes"]' &&
          jsonEncode(pick['memoryIds']) == jsonEncode([selected.id]) &&
          jsonEncode(pick['policyFamilies']) == '[]' &&
          {'AVAILABLE', 'UNCONFIGURED'}.contains(sections['profile']) &&
          sections['memories'] == 'AVAILABLE' &&
          sections['policies'] == 'NOT_REQUESTED' &&
          d['policies'] is List &&
          (d['policies'] as List).isEmpty &&
          d['memories'] is List &&
          (d['memories'] as List).length == 1 &&
          d['comparisonScope'] ==
              'EXPLICIT_ACTIVITY_CATEGORY_PREFERENCE_ONLY' &&
          d['notice'] == selfReviewNotice &&
          d['modelAccess'] == 'UNAVAILABLE' &&
          d['mediaAccess'] == 'UNAVAILABLE' &&
          d['memoryPromotionAllowed'] == false &&
          d['grantsAuthority'] == false &&
          !now.isBefore(at) &&
          end.isAfter(now) &&
          end.difference(at) <= const Duration(minutes: 2) &&
          !end.isAfter(selected.validUntil),
    );
    final m = completionMap(
      (d['memories'] as List).single,
      {'id', 'version', 'summary', 'validUntil'},
      optional: {'memoryType', 'createdAt', 'validFrom', 'confidence'},
    );
    _check(
      m['id'] == selected.id &&
          m['version'] == selected.version &&
          m['summary'] == selected.summary &&
          _same(m['validUntil'], selected.validUntil) &&
          (!m.containsKey('memoryType') ||
              m['memoryType'] == selected.raw['memoryType']),
    );
    for (final key in ['createdAt', 'validFrom']) {
      if (m.containsKey(key)) {
        _check(
          _same(m[key], correctionTime(selected.raw[key])) &&
              !correctionTime(m[key]).isAfter(at),
        );
      }
    }
    if (m.containsKey('confidence')) {
      final score = completionMap(m['confidence'], {'semantics', 'value'});
      _check(score['semantics'] == 'DIRECT_DECLARATION' && score['value'] == 1);
    }
    final profile = completionMap(
      d['profile'],
      sections['profile'] == 'AVAILABLE' ? {'preferredActivityTypes'} : {},
    );
    final categories = <String>[];
    if (sections['profile'] == 'AVAILABLE') {
      final list = profile['preferredActivityTypes'];
      _check(list is List && list.length <= 6);
      for (final c in list) {
        _check(
          c is String &&
              profileCompletionStatements.containsKey(c) &&
              !categories.contains(c),
        );
        categories.add(c);
      }
    }
    final set = completionMap(d['fieldEvidenceSet'], {
      'schemaVersion',
      'owner',
      'observedAt',
      'expiresAt',
      'scope',
      'claims',
      'conflicts',
      'modelAccess',
      'mediaAccess',
      'grantsAuthority',
    });
    final so = completionMap(set['owner'], {'type', 'id'});
    _check(
      set['schemaVersion'] == 'agent-field-evidence-v1' &&
          so['type'] == 'PERSON' &&
          so['id'] == owner &&
          _same(set['observedAt'], at) &&
          _same(set['expiresAt'], end) &&
          set['scope'] == 'COMPLETE_SELECTED_CONTEXT' &&
          set['grantsAuthority'] == false &&
          set['modelAccess'] == 'UNAVAILABLE' &&
          set['mediaAccess'] == 'UNAVAILABLE' &&
          set['claims'] is List &&
          (set['claims'] as List).length <= 100 &&
          set['conflicts'] is List &&
          (set['conflicts'] as List).length <= 100,
    );
    final memoryCategory = selected.category;
    final structured = selected.raw['structuredValue'] as Map;
    // Only validate the server's closed, explicit declaration contract. This
    // does not parse free text, generate new conflicts or choose a winner.
    final comparable =
        memoryCategory != null &&
        structured['activityCategory'] == memoryCategory &&
        (structured['nature'] == 'human-correction' &&
                selected.summary ==
                    correctionNegativeStatements[memoryCategory] ||
            structured['nature'] == 'human-declaration' &&
                selected.summary ==
                    profileCompletionStatements[memoryCategory]);
    final expected = <String>{
      'HUMAN_EXPLICIT_MEMORY:${selected.id}:memory.summary',
    };
    if (comparable) {
      expected.add(
        'HUMAN_EXPLICIT_MEMORY:${selected.id}:preference.activityCategory.$memoryCategory',
      );
    }
    if (sections['profile'] == 'AVAILABLE') {
      expected.add(
        'HUMAN_PRIVATE_PROFILE:${selected.agentID}:profile.preferredActivityTypes',
      );
      for (final category in categories) {
        expected.add(
          'HUMAN_PRIVATE_PROFILE:${selected.agentID}:preference.activityCategory.$category',
        );
      }
    }
    final seen = <String>{}, claims = <String, Map<String, dynamic>>{};
    String? profileSourceKey;
    for (final value in set['claims']) {
      final c = completionMap(
        value,
        {
          'id',
          'itemKind',
          'itemId',
          'subjectKind',
          'subjectId',
          'field',
          'claimantKind',
          'claimantId',
          'source',
          'nature',
          'use',
          'observedAt',
          'collectedAt',
          'collectionEvent',
          'sourceUpdatedAt',
          'declaredAt',
          'captureTimeStatus',
          'validityStatus',
        },
        optional: {
          'selectionField',
          'sourceCreatedAt',
          'validFrom',
          'validUntil',
          'confidence',
        },
      );
      final source = completionMap(c['source'], {
        'kind',
        'id',
        'version',
        'nativeTime',
      });
      final rev = completionMap(source['version'], {'kind', 'revision'});
      final isMemory = source['kind'] == 'HUMAN_EXPLICIT_MEMORY';
      final updated = correctionTime(source['nativeTime']);
      _check(
        (isMemory || source['kind'] == 'HUMAN_PRIVATE_PROFILE') &&
            expected.contains(c['id']) &&
            seen.add(c['id']) &&
            c['id'] == '${source['kind']}:${source['id']}:${c['field']}' &&
            source['id'] == (isMemory ? selected.id : selected.agentID) &&
            rev['kind'] == 'REVISION' &&
            completionVersion(rev['revision']) &&
            (isMemory ? rev['revision'] == selected.version : true) &&
            c['subjectKind'] == 'PERSON' &&
            c['subjectId'] == owner &&
            c['itemKind'] == (isMemory ? 'memories' : 'profile') &&
            c['itemId'] == source['id'] &&
            c['claimantKind'] == 'PERSON_DECLARATION' &&
            c['claimantId'] == owner &&
            c['nature'] == 'USER_DECLARATION' &&
            _same(c['observedAt'], at) &&
            _same(c['collectedAt'], at) &&
            c['collectionEvent'] == 'CURRENT_NATIVE_READ' &&
            _same(c['sourceUpdatedAt'], updated) &&
            _same(c['declaredAt'], updated) &&
            !updated.isAfter(at) &&
            c['captureTimeStatus'] == 'UNKNOWN_NOT_COLLECTED',
      );
      if (isMemory) {
        final use = selected.raw['memoryType'] == 'IDENTITY'
            ? 'NO_IDENTITY_AUTHORITY'
            : {
                'PLACE',
                'CITY',
                'HISTORY',
                'EXPERIENCE',
              }.contains(selected.raw['memoryType'])
            ? 'NO_VISIT_OR_CURRENT_LOCATION_AUTHORITY'
            : 'DECLARATION_ONLY';
        _check(
          !c.containsKey('selectionField') &&
              c['use'] == use &&
              updated == correctionTime(selected.raw['updatedAt']) &&
              c.containsKey('validUntil') &&
              _same(c['validUntil'], selected.validUntil) &&
              c['validityStatus'] ==
                  (m.containsKey('validFrom')
                      ? 'SOURCE_INTERVAL_KNOWN'
                      : 'VALID_UNTIL_KNOWN'),
        );
        for (final pair in {
          'sourceCreatedAt': 'createdAt',
          'validFrom': 'validFrom',
        }.entries) {
          _check(c.containsKey(pair.key) == m.containsKey(pair.value));
          if (c.containsKey(pair.key)) {
            _check(_same(c[pair.key], correctionTime(m[pair.value])));
          }
        }
        _check(c.containsKey('confidence') == m.containsKey('confidence'));
        if (c.containsKey('confidence')) {
          _check(jsonEncode(c['confidence']) == jsonEncode(m['confidence']));
        }
      } else {
        _check(
          c['selectionField'] == 'preferredActivityTypes' &&
              c['use'] == 'DECLARATION_ONLY' &&
              c['validityStatus'] == 'SOURCE_INTERVAL_UNKNOWN' &&
              !c.containsKey('sourceCreatedAt') &&
              !c.containsKey('validFrom') &&
              !c.containsKey('validUntil') &&
              !c.containsKey('confidence'),
        );
        final key = jsonEncode([
          source['kind'],
          source['id'],
          rev['revision'],
          updated.toIso8601String(),
        ]);
        _check(profileSourceKey == null || profileSourceKey == key);
        profileSourceKey = key;
      }
      claims[c['id']] = c;
    }
    _check(seen.length == expected.length && seen.containsAll(expected));
    final conflicts = <String>[];
    for (final value in set['conflicts']) {
      final c = completionMap(value, {
        'subjectKind',
        'subjectId',
        'field',
        'claimIds',
        'status',
        'otherClaimsOmitted',
        'explanation',
      });
      _check(
        c['field'] is String &&
            (c['field'] as String).startsWith('preference.activityCategory.'),
      );
      final category = (c['field'] as String).substring(
        'preference.activityCategory.'.length,
      );
      final pair = {
        'HUMAN_EXPLICIT_MEMORY:${selected.id}:preference.activityCategory.$category',
        'HUMAN_PRIVATE_PROFILE:${selected.agentID}:preference.activityCategory.$category',
      };
      _check(
        c['subjectKind'] == 'PERSON' &&
            c['subjectId'] == owner &&
            c['status'] == 'AWAITING_CONFIRMATION' &&
            c['otherClaimsOmitted'] == false &&
            c['explanation'] == selfReviewConflictExplanation &&
            c['claimIds'] is List &&
            (c['claimIds'] as List).length == 2 &&
            (c['claimIds'] as List).toSet().length == 2 &&
            pair.containsAll(c['claimIds']) &&
            pair.every(claims.containsKey) &&
            comparable &&
            structured['nature'] == 'human-correction' &&
            category == memoryCategory &&
            categories.contains(category) &&
            !conflicts.contains(category),
      );
      conflicts.add(category);
    }
    _check(
      conflicts.length ==
          (comparable &&
                  structured['nature'] == 'human-correction' &&
                  categories.contains(memoryCategory)
              ? 1
              : 0),
    );
    return MemorySelfReview._(
      candidateFreezeJson(d),
      List.unmodifiable(categories),
      List.unmodifiable(conflicts),
    );
  }
}
