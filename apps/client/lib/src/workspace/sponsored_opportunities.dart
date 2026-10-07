import 'package:flutter/material.dart';

const sponsoredTrustVersion = 'sponsored-opportunity-v1';

/// A reviewed commercial declaration, separate from the natural result set.
/// This metadata grants no detail access, action approval or payment authority.
class SponsoredOpportunity {
  const SponsoredOpportunity({
    required this.id,
    required this.revision,
    required this.sponsorID,
    required this.sponsorName,
    required this.targetType,
    required this.targetID,
    required this.title,
    required this.sourceURL,
    required this.observedAt,
    required this.reviewedAt,
    required this.expiresAt,
    required this.checkedAt,
  });
  final String id;
  final int revision;
  final String sponsorID, sponsorName, targetType, targetID, title, sourceURL;
  final DateTime observedAt, reviewedAt, expiresAt, checkedAt;

  String get targetKey => '$targetType:$targetID';
}

class SponsoredDisclosure {
  const SponsoredDisclosure({this.items = const [], this.unavailable = false});
  final List<SponsoredOpportunity> items;
  final bool unavailable;
}

final _sponsoredID = RegExp(
  r'^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$',
);

Never _invalidSponsor() => throw const FormatException('赞助声明格式无效');

Map<String, dynamic> _object(dynamic raw, Set<String> keys) {
  if (raw is! Map<String, dynamic> ||
      raw.keys.toSet().difference(keys).isNotEmpty ||
      keys.difference(raw.keys.toSet()).isNotEmpty) {
    _invalidSponsor();
  }
  return raw;
}

String _text(dynamic raw, int max) {
  if (raw is! String ||
      raw.isEmpty ||
      raw.trim() != raw ||
      raw.runes.length > max ||
      RegExp(r'[\x00-\x1f\x7f]').hasMatch(raw)) {
    _invalidSponsor();
  }
  return raw;
}

String _id(dynamic raw) {
  final s = _text(raw, 36);
  if (!_sponsoredID.hasMatch(s) ||
      s == '00000000-0000-0000-0000-000000000000') {
    _invalidSponsor();
  }
  return s;
}

final _sponsoredUTC = RegExp(
  r'^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.([0-9]{1,9}))?Z$',
);

DateTime _stamp(dynamic raw) {
  if (raw is! String) _invalidSponsor();
  final match = _sponsoredUTC.firstMatch(raw);
  if (match == null || match.end != raw.length) _invalidSponsor();
  final year = int.parse(match.group(1)!);
  final month = int.parse(match.group(2)!);
  final day = int.parse(match.group(3)!);
  final hour = int.parse(match.group(4)!);
  final minute = int.parse(match.group(5)!);
  final second = int.parse(match.group(6)!);
  if (year < 1 || year > 9999) _invalidSponsor();
  // Go's UTC RFC3339Nano wire format can contain nine fractional digits.
  // Dart stores microseconds; truncation must never normalize calendar fields.
  final micros = int.parse(
    (match.group(7) ?? '').padRight(6, '0').substring(0, 6),
  );
  final t = DateTime.utc(year, month, day, hour, minute, second, 0, micros);
  if (t.year != year ||
      t.month != month ||
      t.day != day ||
      t.hour != hour ||
      t.minute != minute ||
      t.second != second) {
    _invalidSponsor();
  }
  return t;
}

/// Legacy responses without any commercial fields remain readable. Once a
/// commercial field exists, the complete closed contract and current natural
/// target reference must match; malformed disclosure is never silently dropped.
SponsoredDisclosure decodeSponsoredDisclosure(
  Map<String, dynamic> envelope, {
  required Map<String, String> eligibleTargets,
}) {
  const fields = {
    'commercialTrustVersion',
    'sponsoredStatus',
    'sponsoredOpportunities',
  };
  if (!fields.any(envelope.containsKey)) return const SponsoredDisclosure();
  if (!fields.every(envelope.containsKey) ||
      envelope['commercialTrustVersion'] != sponsoredTrustVersion ||
      !{'available', 'unavailable'}.contains(envelope['sponsoredStatus'])) {
    _invalidSponsor();
  }
  final rawItems = envelope['sponsoredOpportunities'];
  if (rawItems is! List ||
      rawItems.length > 5 ||
      (envelope['sponsoredStatus'] == 'unavailable' && rawItems.isNotEmpty)) {
    _invalidSponsor();
  }
  final ids = <String>{}, targets = <String>{};
  final items = <SponsoredOpportunity>[];
  for (final raw in rawItems) {
    final row = _object(raw, {
      'id',
      'revision',
      'kind',
      'label',
      'sponsor',
      'target',
      'source',
      'checkedAt',
    });
    if (row['kind'] != 'SPONSORED' ||
        row['label'] != '赞助' ||
        row['revision'] is! int ||
        (row['revision'] as int) < 1 ||
        (row['revision'] as int) > 9007199254740991) {
      _invalidSponsor();
    }
    final sponsor = _object(row['sponsor'], {'type', 'id', 'name'});
    final target = _object(row['target'], {'type', 'id', 'title'});
    final source = _object(row['source'], {
      'url',
      'observedAt',
      'reviewedAt',
      'expiresAt',
    });
    if (sponsor['type'] != 'BUSINESS' ||
        !{'ACTIVITY', 'PLACE'}.contains(target['type'])) {
      _invalidSponsor();
    }
    final id = _id(row['id']), targetID = _id(target['id']);
    final title = _text(target['title'], 240);
    final key = '${target['type']}:$targetID';
    if (!ids.add(id) || !targets.add(key) || eligibleTargets[key] != title) {
      _invalidSponsor();
    }
    final url = _text(source['url'], 2048);
    final parsed = Uri.tryParse(url);
    if (parsed == null ||
        parsed.scheme != 'https' ||
        parsed.host.isEmpty ||
        parsed.userInfo.isNotEmpty ||
        parsed.hasFragment ||
        RegExp(r'\s').hasMatch(url)) {
      _invalidSponsor();
    }
    final observed = _stamp(source['observedAt']);
    final reviewed = _stamp(source['reviewedAt']);
    final expires = _stamp(source['expiresAt']);
    final checked = _stamp(row['checkedAt']);
    // All four timestamps are from the server clock domain, not host wall time.
    if (observed.isAfter(reviewed) ||
        reviewed.isAfter(checked) ||
        !expires.isAfter(checked) ||
        expires.difference(observed) > const Duration(days: 30)) {
      _invalidSponsor();
    }
    items.add(
      SponsoredOpportunity(
        id: id,
        revision: row['revision'] as int,
        sponsorID: _id(sponsor['id']),
        sponsorName: _text(sponsor['name'], 160),
        targetType: target['type'] as String,
        targetID: targetID,
        title: title,
        sourceURL: url,
        observedAt: observed,
        reviewedAt: reviewed,
        expiresAt: expires,
        checkedAt: checked,
      ),
    );
  }
  return SponsoredDisclosure(
    items: List.unmodifiable(items),
    unavailable: envelope['sponsoredStatus'] == 'unavailable',
  );
}

class SponsoredOpportunitiesSection extends StatelessWidget {
  const SponsoredOpportunitiesSection({
    super.key,
    required this.items,
    this.unavailable = false,
    this.onOpen,
    this.canOpen,
  });
  final List<SponsoredOpportunity> items;
  final bool unavailable;
  final ValueChanged<SponsoredOpportunity>? onOpen;
  final bool Function(SponsoredOpportunity)? canOpen;

  @override
  Widget build(BuildContext context) {
    if (items.isEmpty && !unavailable) return const SizedBox.shrink();
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Divider(height: 32),
        Text('赞助展示', style: theme.textTheme.titleMedium),
        const SizedBox(height: 8),
        const Text('商业声明单独展示，不改变自然匹配顺序；付款或合同未经此功能核验。'),
        if (unavailable)
          const Padding(
            padding: EdgeInsets.only(top: 8),
            child: Text('暂时无法核对赞助声明，自然匹配仍可查看。'),
          ),
        for (final item in items)
          Semantics(
            container: true,
            label: '赞助展示，支持方${item.sponsorName}',
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Wrap(
                    spacing: 8,
                    runSpacing: 4,
                    children: [
                      const Chip(label: Text('赞助')),
                      Text(
                        '支持方：${item.sponsorName}',
                        style: theme.textTheme.titleSmall,
                      ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(item.title, style: theme.textTheme.titleMedium),
                  const SizedBox(height: 8),
                  const Text('声明来源（HTTPS）：'),
                  SelectableText(item.sourceURL),
                  const SizedBox(height: 4),
                  Text(
                    '审核：${_date(item.reviewedAt)} · 有效至：${_date(item.expiresAt)}',
                  ),
                  const SizedBox(height: 8),
                  OutlinedButton(
                    onPressed:
                        onOpen == null || (canOpen != null && !canOpen!(item))
                        ? null
                        : () => onOpen!(item),
                    style: OutlinedButton.styleFrom(
                      minimumSize: const Size(48, 48),
                    ),
                    child: Text(
                      item.targetType == 'ACTIVITY' ? '查看赞助活动详情' : '查看赞助地点详情',
                    ),
                  ),
                ],
              ),
            ),
          ),
      ],
    );
  }
}

String _date(DateTime t) {
  final local = t.toLocal();
  return '${local.year}年${local.month}月${local.day}日';
}
