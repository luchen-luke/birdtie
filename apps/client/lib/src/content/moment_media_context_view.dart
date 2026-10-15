import 'dart:async';
import 'package:flutter/material.dart';
import '../workspace/legacy_place_reference_page.dart';
import 'moment_media_context_api.dart';

/// Short lived, owner-only original metadata. Never used as map authority.
class MomentMediaContextView extends StatefulWidget {
  const MomentMediaContextView({
    super.key,
    required this.api,
    required this.momentID,
    required this.changes,
    required this.assetKind,
    required this.visibleAssetIDs,
    this.expectedRevision,
  });
  final MomentMediaContextAPI api;
  final String momentID, assetKind;
  final int? expectedRevision;
  final Listenable changes;
  final Set<String> Function() visibleAssetIDs;
  @override
  State<MomentMediaContextView> createState() => _MomentMediaContextViewState();
}

class _MomentMediaContextViewState extends State<MomentMediaContextView>
    with WidgetsBindingObserver {
  MomentMediaContexts? _value;
  String? _error;
  Timer? _lease;
  int _generation = 0;
  bool _loading = false, _foreground = true;
  String? _token;
  bool get _current =>
      mounted &&
      _foreground &&
      widget.api.current() &&
      _token == widget.api.authorizationHeader();
  @override
  void initState() {
    super.initState();
    _token = widget.api.authorizationHeader();
    widget.changes.addListener(_changed);
    WidgetsBinding.instance.addObserver(this);
    unawaited(_load());
  }

  void _changed() {
    if (!_current) {
      _generation++;
      _lease?.cancel();
      if (mounted)
        setState(() {
          _value = null;
          _loading = false;
        });
    } else if (mounted)
      setState(() {});
  }

  @override
  void didUpdateWidget(covariant MomentMediaContextView old) {
    super.didUpdateWidget(old);
    if (old.changes != widget.changes) {
      old.changes.removeListener(_changed);
      widget.changes.addListener(_changed);
    }
    if (old.momentID != widget.momentID ||
        old.expectedRevision != widget.expectedRevision) {
      _generation++;
      _value = null;
      _loading = false;
      _token = widget.api.authorizationHeader();
      unawaited(_load());
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    if (!_foreground) {
      _generation++;
      _lease?.cancel();
      setState(() {
        _value = null;
        _loading = false;
      });
    } else if (_current)
      unawaited(_load());
  }

  Future<void> _load() async {
    if (!_current || _loading) return;
    final generation = ++_generation;
    _lease?.cancel();
    setState(() {
      _loading = true;
      _value = null;
      _error = null;
    });
    try {
      final value = await widget.api.read(
        widget.momentID,
        expectedRevision: widget.expectedRevision,
      );
      if (!_current || generation != _generation || !value.current) return;
      setState(() => _value = value);
      _lease = Timer(value.validUntil.difference(DateTime.now().toUtc()), () {
        if (mounted && generation == _generation) setState(() => _value = null);
      });
    } catch (_) {
      if (_current && generation == _generation)
        setState(() => _error = '原材料信息暂不可读。');
    } finally {
      if (mounted && generation == _generation)
        setState(() => _loading = false);
    }
  }

  Future<void> _open(MomentMediaContextPlace place) async {
    final value = _value, reference = place.reference;
    if (!_current ||
        value == null ||
        !value.current ||
        reference == null ||
        !reference.current)
      return;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => LegacyPlaceReferencePage(
          reference: reference,
          authorizationHeader: widget.api.authorizationHeader,
          sourceCurrent: () =>
              _current && identical(value, _value) && value.current,
          apiBaseUrl: widget.api.baseUrl,
          client: widget.api.client,
          identityChanges: widget.changes,
        ),
      ),
    );
  }

  @override
  void dispose() {
    _generation++;
    _lease?.cancel();
    widget.changes.removeListener(_changed);
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_current) return const SizedBox.shrink();
    final visible = widget.visibleAssetIDs();
    final items =
        _value?.items
            .where(
              (v) =>
                  v.assetKind == widget.assetKind &&
                  visible.contains(v.assetID),
            )
            .toList() ??
        const <MomentMediaContext>[];
    return ExpansionTile(
      tilePadding: EdgeInsets.zero,
      title: const Text('原材料信息'),
      childrenPadding: const EdgeInsets.only(bottom: 12),
      children: [
        if (_loading) const LinearProgressIndicator(),
        if (_error != null) Text(_error!),
        for (final v in items)
          Align(
            alignment: Alignment.centerLeft,
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 6),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '${v.assetKind == 'IMAGE' ? '图片' : '视频'} ${v.sourcePosition + 1}',
                  ),
                  if (v.capturedDate != null) Text('拍摄日期：${v.capturedDate}'),
                  if (v.coordinateStatus == 'UNSPECIFIED')
                    Text('原坐标：${v.latitude}, ${v.longitude}\n坐标系未记录，仅自己可见。'),
                  for (var i = 0; i < v.locations.length; i++)
                    TextButton.icon(
                      onPressed: v.locations[i].reference?.current == true
                          ? () => _open(v.locations[i])
                          : null,
                      icon: const Icon(Icons.place_outlined),
                      label: Text('查看原关联地点 ${i + 1}'),
                    ),
                ],
              ),
            ),
          ),
        if (!_loading && _value != null && items.isEmpty)
          const Text('没有可读取的原材料信息。'),
        TextButton(
          onPressed: _loading ? null : () => unawaited(_load()),
          child: const Text('刷新材料信息'),
        ),
      ],
    );
  }
}
