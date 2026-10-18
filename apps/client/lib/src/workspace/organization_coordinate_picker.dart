import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import '../app/birdtie_app_bar.dart';
import '../city/public_city_controller.dart';
import '../city/public_city_map.dart';
import '../city/map_provider_policy.dart';
import '../config/birdtie_environment.dart';
import 'now_ui_tokens.dart';

typedef OrganizationDraftCoordinate = ({double latitude, double longitude});
typedef OrganizationCoordinateMapBuilder =
    Widget Function(
      BuildContext context,
      PublicCity city,
      void Function(double latitude, double longitude) onSelected,
      ValueChanged<String> onUnavailable,
    );

String? organizationCoordinateUnavailable(PublicCity city) {
  final provider = mapProviderForCountry(
    countryCode: city.countryCode,
    configuredProvider: city.map?.provider ?? '',
  );
  if (provider == 'amap') {
    if (kIsWeb) {
      return BirdtieEnvironment.amapWebPublicKey.isNotEmpty &&
              BirdtieEnvironment.amapServiceHost.isNotEmpty
          ? null
          : '地图暂不可用，请返回填写获准坐标。';
    }
    // Both existing native adapters return canonical coordinates at the
    // boundary. Key inspection/consent remain the adapter's real SDK gates.
    return const {
          TargetPlatform.android,
          TargetPlatform.iOS,
        }.contains(defaultTargetPlatform)
        ? null
        : '当前平台的坐标选点地图暂不可用，请返回填写获准坐标。';
  }
  if (provider != 'mapbox') return '该城市尚无可用的坐标选点地图。';
  if (kIsWeb) {
    if (!BirdtieEnvironment.mapboxGlWebPublicToken.startsWith('pk.'))
      return '地图暂不可用，请返回填写获准坐标。';
  } else if (!const {
        TargetPlatform.android,
        TargetPlatform.iOS,
      }.contains(defaultTargetPlatform) ||
      !BirdtieEnvironment.mapboxMobilePublicToken.startsWith('pk.')) {
    return '当前平台的坐标选点地图暂不可用，请返回填写获准坐标。';
  }
  return null;
}

/// Returns explicitly selected editable coordinates. It has no domain writer,
/// location permission, public entity creation or publication side effect.
class OrganizationCoordinatePickerPage extends StatefulWidget {
  const OrganizationCoordinatePickerPage({
    super.key,
    required this.city,
    required this.identityChanges,
    required this.sourceCurrent,
    this.mapBuilder,
  });
  final PublicCity city;
  final Listenable identityChanges;
  final bool Function() sourceCurrent;
  final OrganizationCoordinateMapBuilder? mapBuilder;
  @override
  State<OrganizationCoordinatePickerPage> createState() =>
      _OrganizationCoordinatePickerPageState();
}

class _OrganizationCoordinatePickerPageState
    extends State<OrganizationCoordinatePickerPage> {
  Object _mapIdentity(PublicCity city) => (
    city.map?.provider,
    city.map?.latitude,
    city.map?.longitude,
    city.map?.defaultZoom,
    city.map?.sourceRef,
  );
  OrganizationDraftCoordinate? _point;
  bool _retired = false;
  int _revision = 0;
  String? _unavailable;
  // The native SDK keeps this listener for the lifetime of its platform view.
  late final void Function(double, double) _selectCoordinate = _select;
  bool get _current {
    if (!_retired && !widget.sourceCurrent()) {
      _retired = true;
      _point = null;
      ++_revision;
    }
    return mounted && !_retired;
  }

  @override
  void initState() {
    super.initState();
    _unavailable = widget.mapBuilder == null
        ? organizationCoordinateUnavailable(widget.city)
        : null;
    widget.identityChanges.addListener(_observe);
  }

  @override
  void didUpdateWidget(OrganizationCoordinatePickerPage old) {
    super.didUpdateWidget(old);
    if (!identical(old.identityChanges, widget.identityChanges)) {
      old.identityChanges.removeListener(_observe);
      widget.identityChanges.addListener(_observe);
      _retired = true;
    }
    if (old.city.id != widget.city.id ||
        _mapIdentity(old.city) != _mapIdentity(widget.city) ||
        old.mapBuilder != widget.mapBuilder)
      _retired = true;
    if (!_current) _point = null;
  }

  void _observe() {
    if (!mounted) return;
    if (!_current) _point = null;
    setState(() {});
  }

  void _select(double latitude, double longitude) {
    if (!_current ||
        _unavailable != null ||
        !latitude.isFinite ||
        !longitude.isFinite ||
        latitude < -90 ||
        latitude > 90 ||
        longitude < -180 ||
        longitude > 180)
      return;
    setState(() {
      _point = (latitude: latitude, longitude: longitude);
      ++_revision;
    });
  }

  void _mapUnavailable(String message) {
    if (!_current) return;
    setState(() {
      // A failed map cannot leave an earlier point ready for confirmation.
      _point = null;
      ++_revision;
      _unavailable = '地图未能完成选点，请返回填写获准坐标。';
    });
  }

  @override
  void dispose() {
    widget.identityChanges.removeListener(_observe);
    _retired = true;
    _point = null;
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final current = _current, point = _point, revision = _revision;
    final tokens = NowUiTokens.of(context);
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('选择组织公开位置')),
      body: !current
          ? const Center(child: Text('身份或城市已变化，请返回重新选择。'))
          : Column(
              children: [
                Expanded(
                  child: _unavailable != null
                      ? Center(
                          child: Padding(
                            padding: const EdgeInsets.all(24),
                            child: Text(_unavailable!, style: tokens.body),
                          ),
                        )
                      : widget.mapBuilder?.call(
                              context,
                              widget.city,
                              _selectCoordinate,
                              _mapUnavailable,
                            ) ??
                            PublicCityMapView(
                              city: widget.city,
                              places: const [],
                              onPlaceSelected: (_) {},
                              onCoordinateSelected: _selectCoordinate,
                              coordinateSourceCurrent: () => _current,
                              onMapUnavailable: _mapUnavailable,
                              fullBleed: true,
                              contextKey:
                                  'organization-coordinate:${widget.city.id}',
                              preserveViewport: true,
                            ),
                ),
                SafeArea(
                  top: false,
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          _unavailable != null
                              ? '返回位置页填写获准坐标。'
                              : point == null
                              ? '点击地图选择获准公开的位置。'
                              : 'WGS84：${point.latitude.toStringAsFixed(6)}，${point.longitude.toStringAsFixed(6)}',
                          style: tokens.body,
                        ),
                        Text('返回后检查位置与公开范围，再明确提交。', style: tokens.metadata),
                        const SizedBox(height: 8),
                        Wrap(
                          spacing: 12,
                          runSpacing: 8,
                          children: [
                            TextButton(
                              onPressed: () => Navigator.maybePop(context),
                              child: const Text('取消选点'),
                            ),
                            FilledButton(
                              onPressed: point == null
                                  ? null
                                  : () {
                                      if (_current &&
                                          _unavailable == null &&
                                          revision == _revision &&
                                          point == _point)
                                        Navigator.pop<
                                          OrganizationDraftCoordinate
                                        >(context, point);
                                    },
                              child: const Text('使用此坐标填写草稿'),
                            ),
                          ],
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),
    );
  }
}
