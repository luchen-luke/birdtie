/// Native basemap resource failures, independent of tasks and map entities.
enum NativeMapResource { style, source, tile, sprite, glyphs, other }

enum NativeMapFailureReason { denied, rateLimited, network, unavailable }

class NativeMapFailure {
  const NativeMapFailure({
    required this.resource,
    required this.reason,
    required this.safeDiagnostic,
    this.httpStatus,
  });

  factory NativeMapFailure.fromEvent(String resourceType, String message) {
    final resource = switch (resourceType.toUpperCase()) {
      'STYLE' => NativeMapResource.style,
      'SOURCE' => NativeMapResource.source,
      'TILE' => NativeMapResource.tile,
      'SPRITE' => NativeMapResource.sprite,
      'GLYPHS' => NativeMapResource.glyphs,
      _ => NativeMapResource.other,
    };
    final normalized = message.toLowerCase();
    final statusMatch = RegExp(
      r'(?:http(?:\s+status(?:\s+code)?)?|status(?:\s+code)?)\s*[:=]?\s*(\d{3})\b',
      caseSensitive: false,
    ).firstMatch(message);
    final status = statusMatch == null
        ? normalized.contains('forbidden')
              ? 403
              : normalized.contains('unauthorized')
              ? 401
              : null
        : int.tryParse(statusMatch.group(1)!);
    final reason = switch (status) {
      401 || 403 => NativeMapFailureReason.denied,
      429 => NativeMapFailureReason.rateLimited,
      _
          when normalized.contains('network') ||
              normalized.contains('connection') ||
              normalized.contains('timeout') ||
              normalized.contains('timed out') ||
              normalized.contains('resolve host') =>
        NativeMapFailureReason.network,
      _ => NativeMapFailureReason.unavailable,
    };
    return NativeMapFailure(
      resource: resource,
      reason: reason,
      httpStatus: status,
      safeDiagnostic: message
          .replaceAll(RegExp(r'\b(?:pk|sk)\.[A-Za-z0-9._-]+'), '[redacted]')
          .replaceAll(RegExp(r'https?://\S+'), '[url-redacted]'),
    );
  }

  final NativeMapResource resource;
  final NativeMapFailureReason reason;
  final int? httpStatus;
  final String safeDiagnostic;

  String get message {
    final subject = switch (resource) {
      NativeMapResource.tile => '地图瓦片',
      NativeMapResource.style => '地图样式',
      NativeMapResource.source => '地图数据',
      NativeMapResource.sprite || NativeMapResource.glyphs => '地图资源',
      NativeMapResource.other => '地图',
    };
    final status = httpStatus == null ? '' : '（$httpStatus）';
    return switch (reason) {
      NativeMapFailureReason.denied => '$subject请求被地图服务拒绝$status。',
      NativeMapFailureReason.rateLimited => '$subject请求暂时过多$status，请稍后重试。',
      NativeMapFailureReason.network => '$subject连接失败，请检查网络后重试。',
      NativeMapFailureReason.unavailable => '$subject暂未加载成功$status，可重试。',
    };
  }

  String get signature => '${resource.name}:${reason.name}:$httpStatus';
}

/// A failure episode notifies its owner once, regardless of the tile count.
/// Style loading and retrying do not prove that visible tiles have recovered.
class NativeMapFailureState {
  NativeMapFailure? failure;
  bool retrying = false;
  bool _notified = false;

  bool report(String resourceType, String message) {
    final next = NativeMapFailure.fromEvent(resourceType, message);
    final changed = failure?.signature != next.signature || retrying;
    failure = next;
    retrying = false;
    return changed;
  }

  bool takeUnavailableNotification() {
    if (failure == null || _notified) return false;
    _notified = true;
    return true;
  }

  bool beginRetry() {
    if (failure == null || retrying) return false;
    retrying = true;
    return true;
  }

  bool retryFinishedWithoutRecovery() {
    if (!retrying) return false;
    retrying = false;
    return true;
  }

  bool mapLoaded() {
    if (failure == null && !retrying) return false;
    failure = null;
    retrying = false;
    _notified = false;
    return true;
  }
}
