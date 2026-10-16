// Copyright 2023-2024 kuloud
// Copyright 2020 lbs.amap.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,

part of '../amap_map.dart';

final MethodChannelAMapFlutterMap _methodChannel =
    AMapFlutterPlatform.instance as MethodChannelAMapFlutterMap;

/// 地图通信中心
class AMapController {
  final int mapId;
  _MapState? _mapState;
  final List<StreamSubscription<dynamic>> _subscriptions = [];
  bool _disposed = false;

  AMapController._(
    CameraPosition initCameraPosition,
    this._mapState, {
    required this.mapId,
  }) {
    if (_mapState?.mounted == true && !_mapState!._disposed) {
      _connectStreams(mapId);
    } else {
      disponse();
    }
  }

  ///根据传入的id初始化[AMapController]
  /// 主要用于在[AMapWidget]初始化时在[AMapWidget.onMapCreated]中初始化controller
  static Future<AMapController> init(
    int id,
    CameraPosition initialCameration,
    dynamic mapState,
  ) async {
    await _methodChannel.init(id);
    return AMapController._(initialCameration, mapState, mapId: id);
  }

  ///只用于测试
  ///用于与native的通信
  @visibleForTesting
  MethodChannel get channel {
    return _methodChannel.channel(mapId);
  }

  void _connectStreams(int mapId) {
    _listen(_methodChannel.onLocationChanged(mapId: mapId),
        (state, e) => state.widget.onLocationChanged?.call(e.value));

    _listen(_methodChannel.onCameraMove(mapId: mapId), (state, e) {
      state.widget.onCameraMove?.call(e.value);
      if (state.widget.infoWindowAdapter != null) {
        state.updateMarkers();
      }
    });

    _listen(_methodChannel.onCameraMoveEnd(mapId: mapId), (state, e) {
      state.widget.onCameraMoveEnd?.call(e.value);
    });
    _listen(_methodChannel.onMapTap(mapId: mapId),
        (state, e) => state.widget.onTap?.call(e.value));
    _listen(_methodChannel.onMapLongPress(mapId: mapId), (state, e) {
      state.widget.onLongPress?.call(e.value);
    });

    _listen(_methodChannel.onPoiTouched(mapId: mapId), (state, e) {
      state.widget.onPoiTouched?.call(e.value);
    });

    _listen(_methodChannel.onMarkerTap(mapId: mapId), (state, e) {
      state.onMarkerTap(e.value);
    });

    _listen(_methodChannel.onMarkerDragEnd(mapId: mapId), (state, e) {
      state.onMarkerDragEnd(e.value, e.position);
    });

    _listen(_methodChannel.onPolylineTap(mapId: mapId), (state, e) {
      state.onPolylineTap(e.value);
    });
  }

  void _listen<T>(Stream<T> stream, void Function(_MapState, T) onData) {
    _subscriptions.add(stream.listen((event) {
      final state = _mapState;
      if (!_disposed && state != null && state.mounted && !state._disposed) {
        onData(state, event);
      }
    }));
  }

  void disponse() {
    if (_disposed) return;
    _disposed = true;
    for (final subscription in _subscriptions) {
      unawaited(subscription.cancel());
    }
    _subscriptions.clear();
    _mapState = null;
    _methodChannel.dispose(id: mapId);
  }

  Future<void> _updateMapOptions(Map<String, dynamic> optionsUpdate) {
    return _methodChannel.updateMapOptions(optionsUpdate, mapId: mapId);
  }

  Future<void> _updateMarkers(MarkerUpdates markerUpdates) {
    return _methodChannel.updateMarkers(markerUpdates, mapId: mapId);
  }

  Future<void> _updatePolylines(PolylineUpdates polylineUpdates) {
    return _methodChannel.updatePolylines(polylineUpdates, mapId: mapId);
  }

  Future<void> _updatePolygons(PolygonUpdates polygonUpdates) {
    return _methodChannel.updatePolygons(polygonUpdates, mapId: mapId);
  }

  ///改变地图视角
  ///
  ///通过[CameraUpdate]对象设置新的中心点、缩放比例、放大缩小、显示区域等内容
  ///
  ///（注意：iOS端设置显示区域时，不支持duration参数，动画时长使用iOS地图默认值350毫秒）
  ///
  ///可选属性[animated]用于控制是否执行动画移动
  ///
  ///可选属性[duration]用于控制执行动画的时长,默认250毫秒,单位:毫秒
  Future<void> moveCamera(
    CameraUpdate cameraUpdate, {
    bool animated = true,
    int duration = 250,
  }) {
    return _methodChannel.moveCamera(
      cameraUpdate,
      mapId: mapId,
      animated: animated,
      duration: duration,
    );
  }

  /// Sets the screen point used by the native map as its visual center.
  ///
  /// Calling this after [onMapCreated] ensures the platform view has a real
  /// size before Android converts the normalized offset to pixels.
  Future<void> setScreenAnchor(Offset screenAnchor) {
    return _updateMapOptions(<String, dynamic>{
      'screenAnchor': <double>[screenAnchor.dx, screenAnchor.dy],
    });
  }

  /// Caps native map rendering without disposing the platform view.
  ///
  /// Civu uses a low frame rate while an opaque modal editor owns input, then
  /// restores the normal cap when the modal closes.
  Future<void> setRenderFps(int fps) {
    assert(fps >= 1 && fps <= 60);
    return channel.invokeMethod<void>('map#setRenderFps', <String, int>{
      'fps': fps.clamp(1, 60),
    });
  }

  /// Android only: combines sheet occlusion with the Activity lifecycle.
  /// Keeps the map/camera instance; completion is not a rendered-frame signal.
  Future<void> setRenderingPaused(bool paused) {
    return channel.invokeMethod<void>('map#setRenderingPaused', <String, bool>{
      'paused': paused,
    });
  }

  ///地图截屏
  Future<Uint8List?> takeBottomStrip(double fraction) =>
      _methodChannel.takeBottomStrip(mapId: mapId, fraction: fraction);

  Future<Uint8List?> takeSnapshot() {
    return _methodChannel.takeSnapshot(mapId: mapId);
  }

  /// 清空缓存
  Future<void> clearDisk() {
    return _methodChannel.clearDisk(mapId: mapId);
  }

  /// 经纬度转屏幕坐标 since v1.0.3
  Future<ScreenCoordinate> toScreenCoordinate(LatLng latLng) {
    return _methodChannel.toScreenLocation(latLng, mapId: mapId);
  }

  /// 屏幕坐标转经纬度 From v1.0.3
  Future<LatLng> fromScreenCoordinate(ScreenCoordinate screenCoordinate) {
    return _methodChannel.fromScreenLocation(screenCoordinate, mapId: mapId);
  }

  Future<String> getMapContentApprovalNumber() {
    return _methodChannel.getMapContentApprovalNumber(mapId: mapId);
  }

  Future<String> getSatelliteImageApprovalNumber() {
    return _methodChannel.getSatelliteImageApprovalNumber(mapId: mapId);
  }
}
