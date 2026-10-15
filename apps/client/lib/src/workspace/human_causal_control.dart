import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

const humanControlMaxVersion = 8999999999999999;
bool humanControlID(Object? v) =>
    v is String &&
    RegExp(
      r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
    ).hasMatch(v);
bool humanControlVersion(Object? v, {bool zero = true}) =>
    v is int && v >= (zero ? 0 : 1) && v <= humanControlMaxVersion;
bool humanControlHash(Object? v) =>
    v is String && RegExp(r'^[0-9a-f]{64}$').hasMatch(v);
bool humanControlText(String v, int min, int max) =>
    v == v.trim() &&
    !v.contains('\u0000') &&
    v.runes.length >= min &&
    v.runes.length <= max;
String humanControlTextDigest(String v) =>
    sha256.convert(utf8.encode(v)).toString();
String humanControlTupleDigest(List<Object> values) =>
    humanControlTextDigest(jsonEncode(values));
Map<String, dynamic> humanControlObject(
  Object? raw,
  Set<String> required, {
  Set<String> optional = const {},
}) {
  if (raw is! Map<String, dynamic> ||
      !required.every(raw.containsKey) ||
      raw.keys.any((k) => !required.contains(k) && !optional.contains(k)))
    throw const FormatException('原操作资料结构不符');
  return raw;
}

DateTime humanControlTime(Object? v) {
  if (v is! String || !RegExp(r'Z$|[+-]\d\d:\d\d$').hasMatch(v))
    throw const FormatException('资料期限不符');
  final time = DateTime.tryParse(v)?.toUtc();
  if (time == null) throw const FormatException('资料期限不符');
  return time;
}

void humanControlLease(DateTime observed, DateTime expires) {
  final now = DateTime.now().toUtc();
  if (observed.isAfter(now.add(const Duration(seconds: 2))) ||
      !expires.isAfter(now) ||
      !expires.isAfter(observed) ||
      expires.isAfter(observed.add(const Duration(seconds: 31))) ||
      expires.isAfter(now.add(const Duration(seconds: 31))))
    throw const FormatException('当前资料或批准已到期');
}

enum HumanCausalFamily {
  faq('FAQ', '组织问答'),
  context('CONTEXT', '生活资料'),
  report('REPORT', '举报'),
  grantRevoke('GRANT_REVOKE', '资料访问授权'),
  block('BLOCK', '屏蔽');

  const HumanCausalFamily(this.wire, this.label);
  final String wire, label;
  static HumanCausalFamily parse(Object? v) => values.firstWhere(
    (f) => f.wire == v,
    orElse: () => throw const FormatException('操作类别不符'),
  );
  String base(String organizationID) => switch (this) {
    faq =>
      humanControlID(organizationID)
          ? '/v1/me/organizations/$organizationID/faq-control'
          : throw const FormatException('组织来源不符'),
    context => '/v1/me/context-control',
    report => '/v1/me/report-control',
    grantRevoke => '/v1/me/consent-revoke-control',
    block => '/v1/me/block-control',
  };
}

/// Only original selectors survive restart. No text, approval or credentials.
class HumanCausalSelector {
  const HumanCausalSelector({
    required this.family,
    required this.operationID,
    required this.action,
    required this.payloadDigest,
    this.organizationID = '',
    this.targetID = '',
    this.targetType = '',
    this.expectedVersion,
  });
  final HumanCausalFamily family;
  final String operationID,
      action,
      payloadDigest,
      organizationID,
      targetID,
      targetType;
  final int? expectedVersion;
  String get scope => family == HumanCausalFamily.faq ? organizationID : '';
  bool get valid {
    if (!humanControlID(operationID) ||
        !humanControlHash(payloadDigest) ||
        (family == HumanCausalFamily.faq
            ? !humanControlID(organizationID)
            : organizationID.isNotEmpty))
      return false;
    if (family == HumanCausalFamily.report)
      return action == 'SUBMIT' &&
          expectedVersion == null &&
          humanReportTargetTypes.contains(targetType) &&
          (targetType == 'general'
              ? targetID.isEmpty
              : humanControlID(targetID));
    if (!humanControlVersion(expectedVersion) || targetType.isNotEmpty)
      return false;
    return switch (family) {
      HumanCausalFamily.faq =>
        const {'CREATE', 'UPDATE', 'DELETE'}.contains(action) &&
            (action == 'CREATE'
                ? targetID.isEmpty && expectedVersion == 0
                : humanControlID(targetID) && expectedVersion! >= 1) &&
            (action != 'UPDATE' || expectedVersion! < humanControlMaxVersion),
      HumanCausalFamily.context =>
        const {'DECLARE', 'REMOVE'}.contains(action) &&
            (action == 'DECLARE'
                ? targetID.isEmpty && expectedVersion! < humanControlMaxVersion
                : humanControlID(targetID)),
      HumanCausalFamily.grantRevoke =>
        action == 'REVOKE' && humanControlID(targetID) && expectedVersion! >= 1,
      HumanCausalFamily.block =>
        const {'BLOCK', 'UNBLOCK'}.contains(action) &&
            humanControlID(targetID) &&
            (action != 'BLOCK' || expectedVersion! < humanControlMaxVersion),
      HumanCausalFamily.report => false,
    };
  }

  Map<String, Object> toJson() => {
    'family': family.wire,
    'operationId': operationID,
    'action': action,
    'payloadDigest': payloadDigest,
    if (family == HumanCausalFamily.faq) 'organizationId': organizationID,
    'targetId': targetID,
    if (expectedVersion != null) 'expectedVersion': expectedVersion!,
    if (family == HumanCausalFamily.report) 'targetType': targetType,
  };
  bool same(HumanCausalSelector other) => mapEquals(toJson(), other.toJson());
  factory HumanCausalSelector.fromJson(Object? raw) {
    if (raw is! Map<String, dynamic>) throw const FormatException('原操作选择不符');
    final f = HumanCausalFamily.parse(raw['family']);
    final m = humanControlObject(raw, {
      'family',
      'operationId',
      'action',
      'payloadDigest',
      'targetId',
      if (f == HumanCausalFamily.faq) 'organizationId',
      if (f == HumanCausalFamily.report) 'targetType' else 'expectedVersion',
    });
    if (m['operationId'] is! String ||
        m['action'] is! String ||
        m['payloadDigest'] is! String ||
        m['targetId'] is! String ||
        (f == HumanCausalFamily.faq && m['organizationId'] is! String) ||
        (f == HumanCausalFamily.report && m['targetType'] is! String))
      throw const FormatException('原操作选择不符');
    final s = HumanCausalSelector(
      family: f,
      operationID: m['operationId'],
      action: m['action'],
      payloadDigest: m['payloadDigest'],
      organizationID: m['organizationId'] ?? '',
      targetID: m['targetId'],
      targetType: m['targetType'] ?? '',
      expectedVersion: m['expectedVersion'] is int
          ? m['expectedVersion']
          : null,
    );
    if (!s.valid) throw const FormatException('原操作选择不符');
    return s;
  }
}

sealed class HumanCausalInput {
  const HumanCausalInput();
  HumanCausalFamily get family;
  String get operationID;
  String get action;
  String get digest;
  bool get valid;
  Map<String, Object> get fields;
  HumanCausalSelector get selector;
  Map<String, Object> body({String? approval}) {
    if (!valid) throw const FormatException('请检查当前内容、目标和具体版本');
    return {...fields, if (approval != null) 'approval': approval};
  }
}

class HumanFAQInput extends HumanCausalInput {
  const HumanFAQInput({
    required this.organizationID,
    required this.operationID,
    required this.action,
    required this.expectedVersion,
    this.faqID = '',
    this.question = '',
    this.answer = '',
    this.published,
  });
  final String organizationID, faqID, question, answer;
  @override
  final String operationID, action;
  final int expectedVersion;
  final bool? published;
  @override
  HumanCausalFamily get family => HumanCausalFamily.faq;
  @override
  bool get valid =>
      selector.valid &&
      (action == 'DELETE'
          ? question.isEmpty && answer.isEmpty && published == null
          : humanControlText(question, 2, 300) &&
                humanControlText(answer, 1, 3000) &&
                published != null);
  @override
  String get digest => humanControlTupleDigest([
    'human-faq-v1',
    organizationID,
    operationID,
    action,
    expectedVersion,
    faqID,
    humanControlTextDigest(question),
    humanControlTextDigest(answer),
    published ?? false,
  ]);
  @override
  Map<String, Object> get fields => {
    'operationId': operationID,
    'action': action,
    'faqId': faqID,
    'expectedVersion': expectedVersion,
    if (action != 'DELETE') ...{
      'question': question,
      'answer': answer,
      'published': published ?? false,
    },
  };
  @override
  HumanCausalSelector get selector => HumanCausalSelector(
    family: family,
    operationID: operationID,
    action: action,
    payloadDigest: digest,
    organizationID: organizationID,
    targetID: faqID,
    expectedVersion: expectedVersion,
  );
}

const humanContextRelations = {
  'current',
  'past',
  'destination',
  'home',
  'affiliation',
  'interest',
};

class HumanContextInput extends HumanCausalInput {
  const HumanContextInput({
    required this.operationID,
    required this.action,
    required this.expectedVersion,
    required this.relation,
    this.contextID = '',
    this.contextType = '',
    this.sourceKey = '',
    this.localLabel,
  });
  @override
  final String operationID, action;
  final String relation, contextID, contextType, sourceKey;
  // Current readable label is used only in this local confirmation.
  final String? localLabel;
  final int expectedVersion;
  @override
  HumanCausalFamily get family => HumanCausalFamily.context;
  @override
  bool get valid =>
      selector.valid &&
      humanContextRelations.contains(relation) &&
      (action == 'REMOVE'
          ? contextType.isEmpty && sourceKey.isEmpty
          : humanControlText(sourceKey, 1, 160) &&
                !sourceKey.contains('\r') &&
                !sourceKey.contains('\n') &&
                switch (contextType) {
                  'CITY' => const {
                    'current',
                    'past',
                    'destination',
                    'home',
                  }.contains(relation),
                  'INSTITUTION' => const {
                    'past',
                    'affiliation',
                  }.contains(relation),
                  'ONLINE' => relation == 'interest',
                  _ => false,
                });
  @override
  String get digest => humanControlTupleDigest([
    'human-context-v1',
    operationID,
    action,
    expectedVersion,
    contextID,
    contextType,
    humanControlTextDigest(sourceKey),
    relation,
  ]);
  @override
  Map<String, Object> get fields => {
    'operationId': operationID,
    'action': action,
    'expectedVersion': expectedVersion,
    'relation': relation,
    if (action == 'DECLARE') ...{
      'contextType': contextType,
      'sourceKey': sourceKey,
    } else
      'contextId': contextID,
  };
  @override
  HumanCausalSelector get selector => HumanCausalSelector(
    family: family,
    operationID: operationID,
    action: action,
    payloadDigest: digest,
    targetID: contextID,
    expectedVersion: expectedVersion,
  );
}

const humanReportTargetTypes = {
  'general',
  'account',
  'organization',
  'activity',
  'message',
  'community',
  'business',
  'activity_message',
  'community_message',
};
const humanReportReasons = {
  'safety',
  'harassment',
  'incorrect_information',
  'technical',
  'other',
};

class HumanReportInput extends HumanCausalInput {
  const HumanReportInput({
    required this.operationID,
    required this.targetType,
    required this.targetID,
    required this.reason,
    required this.details,
  });
  @override
  final String operationID;
  final String targetType, targetID, reason, details;
  @override
  String get action => 'SUBMIT';
  @override
  HumanCausalFamily get family => HumanCausalFamily.report;
  @override
  bool get valid =>
      selector.valid &&
      humanReportReasons.contains(reason) &&
      humanControlText(details, 10, 1000);
  @override
  String get digest => humanControlTupleDigest([
    'human-report-v1',
    operationID,
    action,
    targetType,
    targetID,
    reason,
    humanControlTextDigest(details),
  ]);
  @override
  Map<String, Object> get fields => {
    'operationId': operationID,
    'action': action,
    'targetType': targetType,
    'targetId': targetID,
    'reason': reason,
    'details': details,
  };
  @override
  HumanCausalSelector get selector => HumanCausalSelector(
    family: family,
    operationID: operationID,
    action: action,
    payloadDigest: digest,
    targetID: targetID,
    targetType: targetType,
  );
}

class HumanGrantInput extends HumanCausalInput {
  const HumanGrantInput({
    required this.operationID,
    required this.grantID,
    required this.expectedVersion,
  });
  @override
  final String operationID;
  final String grantID;
  final int expectedVersion;
  @override
  String get action => 'REVOKE';
  @override
  HumanCausalFamily get family => HumanCausalFamily.grantRevoke;
  @override
  bool get valid => selector.valid;
  @override
  String get digest => humanControlTupleDigest([
    'human-grant-revoke-v1',
    operationID,
    action,
    grantID,
    expectedVersion,
  ]);
  @override
  Map<String, Object> get fields => {
    'operationId': operationID,
    'action': action,
    'grantId': grantID,
    'expectedVersion': expectedVersion,
  };
  @override
  HumanCausalSelector get selector => HumanCausalSelector(
    family: family,
    operationID: operationID,
    action: action,
    payloadDigest: digest,
    targetID: grantID,
    expectedVersion: expectedVersion,
  );
}

class HumanBlockInput extends HumanCausalInput {
  const HumanBlockInput({
    required this.operationID,
    required this.action,
    required this.accountID,
    required this.expectedVersion,
  });
  @override
  final String operationID, action;
  final String accountID;
  final int expectedVersion;
  @override
  HumanCausalFamily get family => HumanCausalFamily.block;
  @override
  bool get valid => selector.valid;
  @override
  String get digest => humanControlTupleDigest([
    'human-block-v1',
    operationID,
    action,
    accountID,
    expectedVersion,
  ]);
  @override
  Map<String, Object> get fields => {
    'operationId': operationID,
    'action': action,
    'accountId': accountID,
    'expectedVersion': expectedVersion,
  };
  @override
  HumanCausalSelector get selector => HumanCausalSelector(
    family: family,
    operationID: operationID,
    action: action,
    payloadDigest: digest,
    targetID: accountID,
    expectedVersion: expectedVersion,
  );
}

class HumanCausalPreview {
  const HumanCausalPreview(
    this.input,
    this.approval,
    this.observedAt,
    this.expiresAt,
  );
  final HumanCausalInput input;
  final String approval;
  final DateTime observedAt, expiresAt;
  factory HumanCausalPreview.fromJson(
    Object? raw,
    HumanCausalInput input,
    DateTime sourceExpiresAt,
  ) {
    final echo = {
      ...input.fields,
      if (input is HumanFAQInput) 'organizationId': input.organizationID,
    };
    final m = humanControlObject(raw, {
      ...echo.keys,
      'payloadDigest',
      'observedAt',
      'expiresAt',
      'approval',
    });
    final observed = humanControlTime(m['observedAt']),
        expires = humanControlTime(m['expiresAt']);
    humanControlLease(observed, expires);
    if (!input.valid ||
        echo.entries.any(
          (e) => m[e.key] != e.value || e.value is int && m[e.key] is! int,
        ) ||
        m['payloadDigest'] != input.digest ||
        m['approval'] is! String ||
        (m['approval'] as String).isEmpty ||
        utf8.encode(m['approval'] as String).length > 8192 ||
        !sourceExpiresAt.isAfter(DateTime.now().toUtc()))
      throw const FormatException('这次批准与原内容、目标、版本或期限不符');
    return HumanCausalPreview(
      input,
      m['approval'],
      observed,
      expires.isBefore(sourceExpiresAt) ? expires : sourceExpiresAt,
    );
  }
}

class HumanCausalReceipt {
  const HumanCausalReceipt({
    required this.selector,
    required this.status,
    required this.appliedVersion,
    this.targetID = '',
    this.resultID = '',
    this.committedAt,
  });
  final HumanCausalSelector selector;
  final String status, targetID, resultID;
  final int appliedVersion;
  final DateTime? committedAt;
  factory HumanCausalReceipt.fromJson(
    Object? raw,
    HumanCausalSelector selector,
  ) {
    if (raw is! Map<String, dynamic> || !selector.valid)
      throw const FormatException('原操作回执不符');
    final stopped = raw['status'] == 'STOPPED', f = selector.family;
    final m = humanControlObject(raw, {
      'operationId',
      'family',
      'status',
      'appliedVersion',
      if (!stopped) ...{
        'action',
        'payloadDigest',
        'committedAt',
        if (f != HumanCausalFamily.report || selector.targetType != 'general')
          'targetId',
        if (const {
          HumanCausalFamily.faq,
          HumanCausalFamily.context,
          HumanCausalFamily.report,
        }.contains(f))
          'resultId',
      },
    });
    if (m['operationId'] != selector.operationID ||
        m['family'] != f.wire ||
        !humanControlVersion(m['appliedVersion']))
      throw const FormatException('原操作类别、编号或结果版本不符');
    final version = m['appliedVersion'] as int;
    if (stopped) {
      if (version != 0) throw const FormatException('原停止回执不符');
      return HumanCausalReceipt(
        selector: selector,
        status: 'STOPPED',
        appliedVersion: 0,
      );
    }
    final target = m['targetId'] ?? '',
        result = m['resultId'] ?? '',
        expected = selector.expectedVersion;
    if (m['status'] != 'SUCCEEDED' ||
        m['action'] != selector.action ||
        m['payloadDigest'] != selector.payloadDigest ||
        target is! String ||
        result is! String)
      throw const FormatException('原操作输入或成功回执不符');
    final next = expected == humanControlMaxVersion
        ? humanControlMaxVersion
        : (expected ?? 0) + 1;
    final valid = switch (f) {
      HumanCausalFamily.faq =>
        humanControlID(target) &&
            target == result &&
            version == (selector.action == 'CREATE' ? 1 : next) &&
            (selector.action == 'CREATE' || target == selector.targetID),
      HumanCausalFamily.context =>
        humanControlID(target) &&
            target == result &&
            (selector.action != 'REMOVE' || target == selector.targetID) &&
            (expected == humanControlMaxVersion
                ? selector.action == 'REMOVE' && version == expected
                : version > expected!),
      HumanCausalFamily.report =>
        humanControlID(result) && version == 0 && target == selector.targetID,
      HumanCausalFamily.grantRevoke || HumanCausalFamily.block =>
        target == selector.targetID && result.isEmpty && version == next,
    };
    if (!valid) throw const FormatException('原操作结果与具体目标不符');
    final committed = humanControlTime(m['committedAt']);
    if (committed.isAfter(
      DateTime.now().toUtc().add(const Duration(seconds: 2)),
    ))
      throw const FormatException('原回执时间不符');
    return HumanCausalReceipt(
      selector: selector,
      status: 'SUCCEEDED',
      appliedVersion: version,
      targetID: target,
      resultID: result,
      committedAt: committed,
    );
  }
}

sealed class HumanCausalSnapshot {
  const HumanCausalSnapshot(this.observedAt, this.expiresAt);
  final DateTime observedAt, expiresAt;
  bool get current => expiresAt.isAfter(DateTime.now().toUtc());
  bool permits(HumanCausalInput input);
}

class HumanFAQSnapshot extends HumanCausalSnapshot {
  HumanFAQSnapshot(
    super.observedAt,
    super.expiresAt,
    this.organizationID,
    this.role,
    this.items,
  );
  final String organizationID, role;
  final List<Map<String, dynamic>> items;
  factory HumanFAQSnapshot.fromJson(Object? raw, String org) {
    final m = humanControlObject(raw, {
          'organizationId',
          'role',
          'items',
          'observedAt',
          'expiresAt',
        }),
        o = humanControlTime(m['observedAt']),
        e = humanControlTime(m['expiresAt']);
    humanControlLease(o, e);
    if (m['organizationId'] != org ||
        !const {'owner', 'admin'}.contains(m['role']) ||
        m['items'] is! List)
      throw const FormatException('当前组织问答权限不符');
    final items = <Map<String, dynamic>>[];
    for (final raw in m['items']) {
      final row = humanControlObject(raw, {
        'id',
        'organizationId',
        'question',
        'answer',
        'published',
        'createdAt',
        'updatedAt',
        'revision',
      });
      if (!humanControlID(row['id']) ||
          row['organizationId'] != org ||
          !humanControlVersion(row['revision'], zero: false) ||
          row['question'] is! String ||
          row['answer'] is! String ||
          row['published'] is! bool)
        throw const FormatException('当前问答资料不符');
      items.add(Map.unmodifiable(row));
    }
    if (items.map((r) => r['id']).toSet().length != items.length)
      throw const FormatException('问答列表不符');
    return HumanFAQSnapshot(o, e, org, m['role'], List.unmodifiable(items));
  }
  @override
  bool permits(HumanCausalInput input) =>
      current &&
      input is HumanFAQInput &&
      input.organizationID == organizationID &&
      (input.action == 'CREATE'
          ? input.expectedVersion == 0
          : items.any(
              (r) =>
                  r['id'] == input.faqID &&
                  r['revision'] == input.expectedVersion,
            ));
}

class HumanContextSnapshot extends HumanCausalSnapshot {
  HumanContextSnapshot(
    super.observedAt,
    super.expiresAt,
    this.version,
    this.items,
  );
  final int version;
  final List<Map<String, dynamic>> items;
  factory HumanContextSnapshot.fromJson(Object? raw) {
    final m = humanControlObject(raw, {
          'version',
          'items',
          'observedAt',
          'expiresAt',
        }),
        o = humanControlTime(m['observedAt']),
        e = humanControlTime(m['expiresAt']);
    humanControlLease(o, e);
    if (!humanControlVersion(m['version']) || m['items'] is! List)
      throw const FormatException('本人生活资料版本不符');
    final items = <Map<String, dynamic>>[];
    for (final raw in m['items']) {
      final row = humanControlObject(raw, {
        'contextId',
        'contextType',
        'sourceKey',
        'label',
        'relation',
        'visibility',
      });
      if (!humanControlID(row['contextId']) ||
          !const {
            'CITY',
            'INSTITUTION',
            'ONLINE',
          }.contains(row['contextType']) ||
          row['sourceKey'] is! String ||
          row['label'] is! String ||
          !humanContextRelations.contains(row['relation']) ||
          row['visibility'] != 'private')
        throw const FormatException('本人生活资料不符');
      items.add(Map.unmodifiable(row));
    }
    return HumanContextSnapshot(o, e, m['version'], List.unmodifiable(items));
  }
  @override
  bool permits(HumanCausalInput input) =>
      current &&
      input is HumanContextInput &&
      input.expectedVersion == version &&
      (input.action == 'DECLARE' ||
          items.any(
            (r) =>
                r['contextId'] == input.contextID &&
                r['relation'] == input.relation,
          ));
}

class HumanReportSnapshot extends HumanCausalSnapshot {
  HumanReportSnapshot(
    super.observedAt,
    super.expiresAt,
    this.remainingQuota,
    this.canSubmit,
  );
  final int remainingQuota;
  final bool canSubmit;
  factory HumanReportSnapshot.fromJson(Object? raw) {
    final m = humanControlObject(raw, {
          'remainingQuota',
          'canSubmit',
          'observedAt',
          'expiresAt',
        }),
        o = humanControlTime(m['observedAt']),
        e = humanControlTime(m['expiresAt']);
    humanControlLease(o, e);
    if (m['remainingQuota'] is! int ||
        m['remainingQuota'] < 0 ||
        m['remainingQuota'] > 5 ||
        m['canSubmit'] is! bool ||
        m['canSubmit'] == true && m['remainingQuota'] == 0)
      throw const FormatException('当前举报提交状态不符');
    return HumanReportSnapshot(o, e, m['remainingQuota'], m['canSubmit']);
  }
  @override
  bool permits(HumanCausalInput input) =>
      current && canSubmit && input is HumanReportInput;
}

class HumanGrantSnapshot extends HumanCausalSnapshot {
  HumanGrantSnapshot(
    super.observedAt,
    super.expiresAt,
    this.grant,
    this.canRevoke,
  );
  final Map<String, dynamic> grant;
  final bool canRevoke;
  factory HumanGrantSnapshot.fromJson(Object? raw, String id) {
    final m = humanControlObject(raw, {
          'grant',
          'canRevoke',
          'observedAt',
          'expiresAt',
        }),
        o = humanControlTime(m['observedAt']),
        e = humanControlTime(m['expiresAt']);
    humanControlLease(o, e);
    final g = humanControlObject(
      m['grant'],
      {
        'id',
        'recipientAccountId',
        'resourceType',
        'resourceId',
        'purpose',
        'actions',
        'revision',
        'createdAt',
      },
      optional: {'expiresAt', 'revokedAt'},
    );
    if (g['id'] != id ||
        !humanControlID(g['recipientAccountId']) ||
        !humanControlVersion(g['revision'], zero: false) ||
        g['resourceType'] is! String ||
        g['resourceId'] is! String ||
        g['purpose'] is! String ||
        g['actions'] is! List ||
        !(g['actions'] as List).every((v) => v is String) ||
        m['canRevoke'] is! bool)
      throw const FormatException('本人资料授权不符');
    if (g['expiresAt'] != null) humanControlTime(g['expiresAt']);
    if (g['revokedAt'] != null) humanControlTime(g['revokedAt']);
    return HumanGrantSnapshot(o, e, Map.unmodifiable(g), m['canRevoke']);
  }
  @override
  bool permits(HumanCausalInput input) =>
      current &&
      canRevoke &&
      input is HumanGrantInput &&
      input.grantID == grant['id'] &&
      input.expectedVersion == grant['revision'];
}

class HumanBlockSnapshot extends HumanCausalSnapshot {
  HumanBlockSnapshot(
    super.observedAt,
    super.expiresAt,
    this.accountID,
    this.version,
    this.blocked,
    this.canBlock,
    this.canUnblock,
  );
  final String accountID;
  final int version;
  final bool blocked, canBlock, canUnblock;
  factory HumanBlockSnapshot.fromJson(Object? raw, String id) {
    final m = humanControlObject(raw, {
          'accountId',
          'version',
          'blocked',
          'canBlock',
          'canUnblock',
          'observedAt',
          'expiresAt',
        }),
        o = humanControlTime(m['observedAt']),
        e = humanControlTime(m['expiresAt']);
    humanControlLease(o, e);
    if (m['accountId'] != id ||
        !humanControlVersion(m['version']) ||
        m['blocked'] is! bool ||
        m['canBlock'] is! bool ||
        m['canUnblock'] is! bool)
      throw const FormatException('当前屏蔽关系不符');
    return HumanBlockSnapshot(
      o,
      e,
      id,
      m['version'],
      m['blocked'],
      m['canBlock'],
      m['canUnblock'],
    );
  }
  @override
  bool permits(HumanCausalInput input) =>
      current &&
      input is HumanBlockInput &&
      input.accountID == accountID &&
      input.expectedVersion == version &&
      (input.action == 'BLOCK' ? canBlock : canUnblock);
}

class HumanCausalFailure implements Exception {
  const HumanCausalFailure(this.status, {this.code = ''});
  final int status;
  final String code;
  bool get legacyMomentPhotoReselectionRequired =>
      status == 409 && code == 'legacy_moment_photo_reselection_required';
  String get message {
    if (legacyMomentPhotoReselectionRequired) {
      return '这张旧版配图需要重新选择后才能修改公开动态；也可以添加正文并明确移除配图。';
    }
    return switch (status) {
      401 => '当前个人登录已失效。',
      403 => '当前没有此项操作权限。',
      404 => '当前资料或原操作不可读。',
      409 => '当前资料、权限或具体版本已变化，请重新读取。',
      _ => '服务暂不可用，请稍后读取。',
    };
  }
}

class HumanCausalApi {
  const HumanCausalApi({
    required this.client,
    required this.baseUrl,
    required this.authorizationHeader,
  });
  final http.Client client;
  final String baseUrl;
  final String? Function() authorizationHeader;
  String get origin => baseUrl.replaceFirst(RegExp(r'/$'), '');
  Future<Object?> json(
    String method,
    String path, {
    Map<String, Object>? body,
    required bool Function() current,
  }) async {
    final base = Uri.tryParse(baseUrl), token = authorizationHeader();
    if (base == null ||
        !const {'http', 'https'}.contains(base.scheme) ||
        base.host.isEmpty ||
        base.userInfo.isNotEmpty ||
        base.hasQuery ||
        base.hasFragment ||
        token == null ||
        !current())
      throw const HumanCausalFailure(401);
    final url = Uri.parse('$origin$path');
    if (url.origin != base.origin) throw const FormatException('操作读取来源不符');
    final req = http.Request(method, url)
      ..followRedirects = false
      ..headers['Authorization'] = token;
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final response = await client
            .send(req)
            .timeout(const Duration(seconds: 12)),
        bytes = BytesBuilder(copy: false);
    await for (final block in response.stream.timeout(
      const Duration(seconds: 12),
    )) {
      if (block.length > 1024 * 1024 - bytes.length)
        throw const FormatException('响应过大');
      bytes.add(block);
    }
    if (!current() || token != authorizationHeader())
      throw const HumanCausalFailure(401);
    if (response.statusCode != 200) {
      var code = '';
      if (response.statusCode == 409) {
        try {
          final body = jsonDecode(utf8.decode(bytes.takeBytes()));
          if (body is Map<String, dynamic>) {
            final error = body['error'];
            if (error is Map<String, dynamic> &&
                error['code'] == 'legacy_moment_photo_reselection_required') {
              code = 'legacy_moment_photo_reselection_required';
            }
          }
        } catch (_) {
          // Error bodies never supply display text or executable recovery data.
        }
      }
      throw HumanCausalFailure(response.statusCode, code: code);
    }
    return humanControlObject(jsonDecode(utf8.decode(bytes.takeBytes())), {
      'data',
    })['data'];
  }

  Future<String> owner(bool Function() current) async {
    final raw = await json('GET', '/v1/me', current: current);
    if (raw is! Map<String, dynamic> ||
        !humanControlID(raw['id']) ||
        raw.containsKey('accountType') &&
            !const {'person', 'PERSON'}.contains(raw['accountType']))
      throw const FormatException('当前个人账号不符');
    return raw['id'];
  }

  Future<HumanCausalSnapshot> state(
    HumanCausalFamily family, {
    String organizationID = '',
    String targetID = '',
    required bool Function() current,
  }) async {
    if (const {
          HumanCausalFamily.grantRevoke,
          HumanCausalFamily.block,
        }.contains(family) &&
        !humanControlID(targetID))
      throw const FormatException('当前目标不符');
    final raw = await json(
      'GET',
      '${family.base(organizationID)}${const {HumanCausalFamily.grantRevoke, HumanCausalFamily.block}.contains(family) ? '/$targetID' : ''}',
      current: current,
    );
    return switch (family) {
      HumanCausalFamily.faq => HumanFAQSnapshot.fromJson(raw, organizationID),
      HumanCausalFamily.context => HumanContextSnapshot.fromJson(raw),
      HumanCausalFamily.report => HumanReportSnapshot.fromJson(raw),
      HumanCausalFamily.grantRevoke => HumanGrantSnapshot.fromJson(
        raw,
        targetID,
      ),
      HumanCausalFamily.block => HumanBlockSnapshot.fromJson(raw, targetID),
    };
  }

  Future<HumanCausalPreview> preview(
    HumanCausalInput input,
    DateTime expiry,
    bool Function() current,
  ) async => HumanCausalPreview.fromJson(
    await json(
      'POST',
      '${input.family.base(input.selector.organizationID)}/previews',
      body: input.body(),
      current: current,
    ),
    input,
    expiry,
  );
  Future<HumanCausalReceipt> apply(
    HumanCausalPreview p,
    bool Function() current,
  ) async {
    if (!p.expiresAt.isAfter(DateTime.now().toUtc()))
      throw const FormatException('这次批准已到期');
    return HumanCausalReceipt.fromJson(
      await json(
        'PUT',
        '${p.input.family.base(p.input.selector.organizationID)}/operations/${p.input.operationID}',
        body: p.input.body(approval: p.approval),
        current: current,
      ),
      p.input.selector,
    );
  }

  Future<HumanCausalReceipt> operation(
    HumanCausalSelector s,
    bool Function() current, {
    bool stop = false,
  }) async => HumanCausalReceipt.fromJson(
    await json(
      stop ? 'POST' : 'GET',
      '${s.family.base(s.organizationID)}/operations/${s.operationID}${stop ? '/stop' : ''}',
      current: current,
    ),
    s,
  );
}
