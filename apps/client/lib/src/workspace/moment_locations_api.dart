import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';
import '../content/moment_locations.dart';
import 'human_causal_control.dart';

class MomentLocationsAPI {
  MomentLocationsAPI({
    required this.authorizationHeader,
    required this.current,
    String? apiBaseUrl,
    http.Client? client,
  }) : base = (apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl).replaceFirst(
         RegExp(r'/$'),
         '',
       ),
       _client = client ?? http.Client(),
       _ownsClient = client == null;
  final String? Function() authorizationHeader;
  final bool Function() current;
  final String base;
  final http.Client _client;
  final bool _ownsClient;
  bool _closed = false;
  Future<MomentLocationsView> read(
    String momentID, {
    bool own = false,
    int? expectedRevision,
  }) async {
    final token = authorizationHeader();
    if (_closed ||
        !current() ||
        !humanControlID(momentID) ||
        own && token == null)
      throw const FormatException('动态位置当前不可读取');
    final response = await _client
        .get(
          Uri.parse('$base/v1/${own ? 'me/' : ''}moments/$momentID/locations'),
          headers: {
            'X-Birdtie-Map-Locations': 'locations-v2',
            if (token != null) 'Authorization': token,
          },
        )
        .timeout(const Duration(seconds: 12));
    if (_closed ||
        !current() ||
        authorizationHeader() != token ||
        response.statusCode != 200 ||
        response.bodyBytes.length > 512 * 1024)
      throw const FormatException('动态位置当前不可读取');
    final envelope = humanControlObject(
      jsonDecode(utf8.decode(response.bodyBytes)),
      {'data'},
    );
    return MomentLocationsView.decode(
      envelope['data'],
      momentID,
      own: own,
      expectedRevision: expectedRevision,
    );
  }

  void dispose() {
    if (_closed) return;
    _closed = true;
    if (_ownsClient) _client.close();
  }
}

/// Selectors are local draft material. Labels supplied here live only in the
/// captured actor's RAM; authoritative names are checked again in the preview.
class MomentLocationChips extends StatelessWidget {
  const MomentLocationChips({
    super.key,
    required this.locations,
    this.labels = const {},
    this.onRemove,
    this.onSetPrecision,
    this.onMove,
  });
  final List<MomentLocationInput> locations;
  final Map<String, String> labels;
  final ValueChanged<String>? onRemove;
  final void Function(String, String)? onSetPrecision;
  final void Function(int, int)? onMove;
  @override
  Widget build(BuildContext context) => Wrap(
    spacing: 8,
    runSpacing: 4,
    children: [
      for (var i = 0; i < locations.length; i++)
        PopupMenuButton<String>(
          tooltip: '修改位置公开范围或移除',
          enabled: onSetPrecision != null || onRemove != null || onMove != null,
          onSelected: (value) {
            final item = locations[i];
            if (value == 'remove') {
              onRemove?.call(item.id);
            } else if (value == 'before') {
              onMove?.call(i, i - 1);
            } else if (value == 'after') {
              onMove?.call(i, i + 1);
            } else {
              onSetPrecision?.call(item.id, value);
            }
          },
          itemBuilder: (_) => [
            if (onSetPrecision != null)
              for (final precision in [
                'none',
                if (locations[i].kind != 'REFERENCE') 'city',
                if (locations[i].kind != 'CITY') 'place',
              ])
                PopupMenuItem(
                  value: precision,
                  child: Text(momentLocationPrecisionLabel(precision)),
                ),
            if (onMove != null && i > 0)
              const PopupMenuItem(value: 'before', child: Text('向前移动')),
            if (onMove != null && i + 1 < locations.length)
              const PopupMenuItem(value: 'after', child: Text('向后移动')),
            if (onRemove != null)
              const PopupMenuItem(value: 'remove', child: Text('移除这处位置')),
          ],
          child: Chip(
            label: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 260),
              child: Text(
                '${labels[locations[i].id] ?? (locations[i].kind == 'CITY' ? '已选城市' : '已选地点')} · ${momentLocationPrecisionLabel(locations[i].precision)}',
              ),
            ),
          ),
        ),
    ],
  );
}

class MomentLocationsSidecar extends StatefulWidget {
  const MomentLocationsSidecar({
    super.key,
    required this.momentID,
    required this.authorizationHeader,
    required this.sourceCurrent,
    this.expectedRevision,
    this.apiBaseUrl,
    this.client,
    this.identityChanges,
    this.onOpenPlace,
  });
  final String momentID;
  final int? expectedRevision;
  final String? Function() authorizationHeader;
  final bool Function() sourceCurrent;
  final String? apiBaseUrl;
  final http.Client? client;
  final Listenable? identityChanges;
  final ValueChanged<String>? onOpenPlace;
  @override
  State<MomentLocationsSidecar> createState() => _MomentLocationsSidecarState();
}

class _MomentLocationsSidecarState extends State<MomentLocationsSidecar>
    with WidgetsBindingObserver {
  MomentLocationsView? _value;
  String? _error;
  int _epoch = 0;
  Timer? _timer;
  bool _background = false;
  late String? _token = widget.authorizationHeader();
  late MomentLocationsAPI _api = _createAPI();
  MomentLocationsAPI _createAPI() => MomentLocationsAPI(
    authorizationHeader: () => widget.authorizationHeader(),
    current: () => mounted && !_background && widget.sourceCurrent(),
    apiBaseUrl: widget.apiBaseUrl,
    client: widget.client,
  );
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    widget.identityChanges?.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    if (!mounted) return;
    if (_token == widget.authorizationHeader() && widget.sourceCurrent())
      return;
    _token = widget.authorizationHeader();
    ++_epoch;
    _timer?.cancel();
    setState(() {
      _value = null;
      _error = null;
    });
    if (widget.sourceCurrent() && !_background) unawaited(_load());
  }

  @override
  void didUpdateWidget(covariant MomentLocationsSidecar old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_changed);
      widget.identityChanges?.addListener(_changed);
    }
    if (old.momentID != widget.momentID ||
        old.expectedRevision != widget.expectedRevision ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl) {
      ++_epoch;
      _timer?.cancel();
      _value = null;
      _api.dispose();
      _api = _createAPI();
      unawaited(_load());
    } else {
      _changed();
    }
  }

  Future<void> _load() async {
    final epoch = ++_epoch, token = widget.authorizationHeader();
    _timer?.cancel();
    if (!widget.sourceCurrent() || _background) return;
    setState(() {
      _value = null;
      _error = null;
    });
    try {
      final value = await _api.read(
        widget.momentID,
        expectedRevision: widget.expectedRevision,
      );
      if (!mounted ||
          epoch != _epoch ||
          token != widget.authorizationHeader() ||
          !widget.sourceCurrent() ||
          _background)
        return;
      setState(() => _value = value);
      _timer = Timer(value.validUntil.difference(DateTime.now().toUtc()), () {
        if (mounted && epoch == _epoch)
          setState(() {
            _value = null;
            _error = '位置信息已到期';
          });
      });
    } catch (_) {
      if (mounted &&
          epoch == _epoch &&
          token == widget.authorizationHeader() &&
          widget.sourceCurrent())
        setState(() => _error = '位置暂时无法读取');
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _background = state != AppLifecycleState.resumed;
    if (_background) {
      ++_epoch;
      _timer?.cancel();
      if (mounted) setState(() => _value = null);
    } else if (widget.sourceCurrent()) {
      unawaited(_load());
    }
  }

  @override
  void dispose() {
    ++_epoch;
    _timer?.cancel();
    widget.identityChanges?.removeListener(_changed);
    WidgetsBinding.instance.removeObserver(this);
    _api.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.sourceCurrent() || _background) return const SizedBox.shrink();
    final value = _value;
    if (value == null || !value.current) {
      return _error == null
          ? const SizedBox.shrink()
          : TextButton(onPressed: _load, child: Text('$_error · 重新读取'));
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        for (final item in value.items)
          if (item.current)
            TextButton.icon(
              icon: Icon(
                item.precision == 'place'
                    ? Icons.place_outlined
                    : Icons.location_city_outlined,
                size: 18,
              ),
              label: Text(item.label),
              onPressed: item.kind == 'REFERENCE'
                  ? () {
                      if (!item.current ||
                          !value.current ||
                          !widget.sourceCurrent())
                        return;
                      showDialog<void>(
                        context: context,
                        builder: (context) => AlertDialog(
                          title: Text(item.label),
                          content: SingleChildScrollView(
                            child: Text(
                              '${item.anchor == null ? '' : '${item.anchor!.latitude}, ${item.anchor!.longitude} · WGS84\n'}${item.attribution}\n${item.sourceURL}',
                            ),
                          ),
                          actions: [
                            TextButton(
                              onPressed: () => Navigator.pop(context),
                              child: const Text('关闭'),
                            ),
                          ],
                        ),
                      );
                    }
                  : item.kind == 'PLACE' &&
                        item.precision == 'place' &&
                        widget.onOpenPlace != null
                  ? () {
                      if (value.current &&
                          item.current &&
                          widget.sourceCurrent() &&
                          _token == widget.authorizationHeader())
                        widget.onOpenPlace!(item.placeID);
                    }
                  : null,
            ),
      ],
    );
  }
}
