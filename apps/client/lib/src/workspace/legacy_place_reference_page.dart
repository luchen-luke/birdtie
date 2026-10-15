import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import 'human_causal_control.dart';
import 'now_ui_tokens.dart';

/// A short, owner-bound read receipt. It is never public Place authority.
class LegacyPlaceReference {
  const LegacyPlaceReference({
    required this.kind,
    required this.recordID,
    required this.placeID,
    required this.observedAt,
    required this.validUntil,
    required this.sourceVersion,
  });
  final String kind, recordID, placeID, sourceVersion;
  final DateTime observedAt, validUntil;
  bool get current => DateTime.now().toUtc().isBefore(validUntil);

  factory LegacyPlaceReference.decode(Object? raw) {
    final v = humanControlObject(raw, {
      'kind',
      'recordId',
      'placeId',
      'observedAt',
      'validUntil',
      'sourceVersion',
    });
    if (!const {'saved_place', 'theme_map', 'moment'}.contains(v['kind']) ||
        !humanControlID(v['recordId']) ||
        !humanControlID(v['placeId']) ||
        !humanControlHash(v['sourceVersion']) ||
        v['observedAt'] is! String ||
        v['validUntil'] is! String) {
      throw const FormatException('地点引用无效');
    }
    final observed = DateTime.parse(v['observedAt']).toUtc();
    final expires = DateTime.parse(v['validUntil']).toUtc();
    if (!expires.isAfter(observed) ||
        expires.difference(observed) > const Duration(seconds: 30) ||
        observed.isAfter(
          DateTime.now().toUtc().add(const Duration(seconds: 5)),
        )) {
      throw const FormatException('地点引用已变化');
    }
    return LegacyPlaceReference(
      kind: v['kind'],
      recordID: v['recordId'],
      placeID: v['placeId'],
      observedAt: observed,
      validUntil: expires,
      sourceVersion: v['sourceVersion'],
    );
  }
}

class LegacyPlaceReferencePage extends StatefulWidget {
  const LegacyPlaceReferencePage({
    super.key,
    required this.reference,
    required this.authorizationHeader,
    required this.sourceCurrent,
    required this.apiBaseUrl,
    required this.client,
    this.identityChanges,
  });
  final LegacyPlaceReference reference;
  final String? Function() authorizationHeader;
  final bool Function() sourceCurrent;
  final String apiBaseUrl;
  final http.Client client;
  final Listenable? identityChanges;
  @override
  State<LegacyPlaceReferencePage> createState() =>
      _LegacyPlaceReferencePageState();
}

class _LegacyPlaceReferencePageState extends State<LegacyPlaceReferencePage>
    with WidgetsBindingObserver {
  Map<String, dynamic>? _detail;
  Timer? _expiry;
  String? _token, _error;
  bool _loading = false, _retired = false;
  int _serial = 0;
  bool get _current =>
      !_retired &&
      widget.sourceCurrent() &&
      _token != null &&
      _token == widget.authorizationHeader();

  @override
  void initState() {
    super.initState();
    _token = widget.authorizationHeader();
    widget.identityChanges?.addListener(_boundary);
    WidgetsBinding.instance.addObserver(this);
    unawaited(_load());
  }

  void _boundary() {
    if (_current) return;
    _retired = true;
    _serial++;
    _expiry?.cancel();
    if (mounted)
      setState(() {
        _detail = null;
        _loading = false;
        _error = '登录状态已变化，请返回后重新打开。';
      });
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _boundary();
      if (_current) unawaited(_load());
    } else {
      _serial++;
      _expiry?.cancel();
      if (mounted)
        setState(() {
          _detail = null;
          _loading = false;
        });
    }
  }

  Future<void> _load() async {
    _boundary();
    if (!_current || _loading) return;
    final serial = ++_serial;
    _expiry?.cancel();
    setState(() {
      _loading = true;
      _detail = null;
      _error = null;
    });
    try {
      final ref = widget.reference;
      final uri = Uri.parse(
        '${widget.apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}/v1/me/place-references/${ref.placeID}',
      ).replace(queryParameters: {'kind': ref.kind, 'recordId': ref.recordID});
      final response = await widget.client
          .get(uri, headers: {'Authorization': _token!})
          .timeout(const Duration(seconds: 12));
      if (!mounted || !_current || serial != _serial) return;
      if (response.statusCode != 200 || response.bodyBytes.length > 65536) {
        throw const FormatException('地点资料暂不可读，请刷新或返回。');
      }
      final envelope =
          jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
      final detail = envelope['data'] as Map<String, dynamic>;
      final proof = LegacyPlaceReference.decode(detail['reference']);
      if (detail['schemaVersion'] != 'owned-legacy-place-reference-v1' ||
          proof.kind != ref.kind ||
          proof.recordID != ref.recordID ||
          proof.placeID != ref.placeID ||
          !proof.current ||
          !const [
            'name',
            'categoryCode',
            'summary',
            'addressLabel',
            'cityId',
            'cityName',
          ].every(
            (key) =>
                detail[key] is String &&
                (detail[key] as String).length <= 16384,
          )) {
        throw const FormatException('地点资料已变化，请重新读取。');
      }
      setState(() => _detail = detail);
      _expiry = Timer(proof.validUntil.difference(DateTime.now().toUtc()), () {
        if (mounted && serial == _serial)
          setState(() {
            _detail = null;
            _error = '请刷新地点资料。';
          });
      });
    } catch (_) {
      if (mounted && _current && serial == _serial)
        setState(() => _error = '地点资料暂不可读，请刷新或返回。');
    } finally {
      if (mounted && serial == _serial) setState(() => _loading = false);
    }
  }

  @override
  void dispose() {
    _serial++;
    _expiry?.cancel();
    widget.identityChanges?.removeListener(_boundary);
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.of(context);
    final detail = _current ? _detail : null;
    return Scaffold(
      appBar: AppBar(title: const Text('已保存地点')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            if (_loading) const LinearProgressIndicator(),
            if (detail != null) ...[
              Text(detail['name'] as String, style: tokens.sheetTitle),
              const SizedBox(height: 8),
              Text('Civu 保存资料 · 仅本人可见', style: tokens.metadata),
              if ((detail['cityName'] as String).isNotEmpty) ...[
                const SizedBox(height: 16),
                Text(detail['cityName'] as String, style: tokens.body),
              ],
              if ((detail['addressLabel'] as String).isNotEmpty) ...[
                const SizedBox(height: 12),
                Text(detail['addressLabel'] as String, style: tokens.body),
              ],
              if ((detail['summary'] as String).isNotEmpty) ...[
                const SizedBox(height: 16),
                Text(detail['summary'] as String, style: tokens.body),
              ],
            ],
            if (_error != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 16),
                child: Text(_error!),
              ),
            if (_current)
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: _loading ? null : _load,
                  icon: const Icon(Icons.refresh),
                  label: const Text('刷新资料'),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
