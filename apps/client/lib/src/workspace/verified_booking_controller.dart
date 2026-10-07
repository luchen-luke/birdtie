import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';
import 'booking_analytics_api.dart';

typedef _BookingAuthority = ({String? token, String? workspace, int epoch});

class VerifiedBookingMetadata {
  const VerifiedBookingMetadata({
    required this.placeID,
    required this.support,
    required this.bookingURL,
    required this.sourceURL,
    required this.reviewedAt,
    required this.expiresAt,
    required this.sourceVersion,
    required this.sourceRevision,
    required this.observedAt,
    required this.validUntil,
  });
  final String placeID, support;
  final Uri? bookingURL;
  final Uri sourceURL;
  final DateTime reviewedAt, expiresAt;
  final String sourceVersion, sourceRevision;
  final DateTime observedAt, validUntil;

  static Uri? https(String? value) {
    if (value == null ||
        value.isEmpty ||
        value.length > 2048 ||
        value.contains(RegExp(r'[\x00-\x20\x7f]'))) {
      return null;
    }
    final uri = Uri.tryParse(value);
    if (uri == null ||
        uri.scheme != 'https' ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        uri.hasFragment) {
      return null;
    }
    return uri;
  }

  static DateTime _date(dynamic raw) {
    if (raw is! String ||
        !RegExp(
          r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$',
        ).hasMatch(raw)) {
      throw const FormatException();
    }
    // DateTime.parse normalizes out-of-calendar dates. Reject those shapes.
    final year = int.parse(raw.substring(0, 4)),
        month = int.parse(raw.substring(5, 7)),
        day = int.parse(raw.substring(8, 10));
    final calendar = DateTime.utc(year, month, day);
    if (year < 1 ||
        calendar.year != year ||
        calendar.month != month ||
        calendar.day != day ||
        int.parse(raw.substring(11, 13)) > 23 ||
        int.parse(raw.substring(14, 16)) > 59 ||
        int.parse(raw.substring(17, 19)) > 59) {
      throw const FormatException();
    }
    if (!raw.endsWith('Z')) {
      final offset = raw.substring(raw.length - 6);
      if (int.parse(offset.substring(1, 3)) > 23 ||
          int.parse(offset.substring(4, 6)) > 59) {
        throw const FormatException();
      }
    }
    final parsed = DateTime.parse(raw).toUtc();
    if (parsed.year < 1 || parsed.year > 9999) throw const FormatException();
    return parsed;
  }

  factory VerifiedBookingMetadata.fromJson(
    Map<String, dynamic> json,
    String placeID,
    DateTime now,
  ) {
    final support = json['reservationSupport'];
    final source = https(
      json['sourceUrl'] is String ? json['sourceUrl'] as String : null,
    );
    final booking = https(
      json['reservationUrl'] is String
          ? json['reservationUrl'] as String
          : null,
    );
    final reviewed = _date(json['reviewedAt']),
        expires = _date(json['expiresAt']);
    final observed = _date(json['bookingObservedAt']),
        until = _date(json['bookingValidUntil']);
    final version = json['bookingSourceVersion'];
    final revision = json['bookingSourceRevision'];
    if (json['placeId'] != placeID ||
        !['unknown', 'none', 'contact', 'external_url'].contains(support) ||
        source == null ||
        reviewed.isAfter(now) ||
        !expires.isAfter(now) ||
        !expires.isAfter(reviewed) ||
        version is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(version) ||
        revision is! String ||
        !RegExp(r'^[0-9a-f]{64}$').hasMatch(revision) ||
        observed.isAfter(now.add(const Duration(seconds: 5))) ||
        !until.isAfter(observed) ||
        !until.isAfter(now) ||
        until.isAfter(observed.add(const Duration(seconds: 30))) ||
        until.isAfter(expires) ||
        (support == 'external_url' && booking == null) ||
        (support != 'external_url' && json['reservationUrl'] != null)) {
      throw const FormatException();
    }
    return VerifiedBookingMetadata(
      placeID: placeID,
      support: support as String,
      bookingURL: booking,
      sourceURL: source,
      reviewedAt: reviewed,
      expiresAt: expires,
      sourceVersion: version,
      sourceRevision: revision,
      observedAt: observed,
      validUntil: until,
    );
  }

  bool sameMaterial(VerifiedBookingMetadata other) =>
      placeID == other.placeID &&
      support == other.support &&
      bookingURL == other.bookingURL &&
      sourceURL == other.sourceURL &&
      reviewedAt == other.reviewedAt &&
      expiresAt == other.expiresAt;
}

class BookingPreview {
  BookingPreview._(this.metadata, this._authority, this._generation);
  final VerifiedBookingMetadata metadata;
  final _BookingAuthority _authority;
  final int _generation;
}

/// Reads only the already approved public Venue projection. A preview is local
/// UI intent, not authority, booking confirmation or a provider availability fact.
class VerifiedBookingController extends ChangeNotifier {
  VerifiedBookingController({
    required this.placeID,
    required this.authorizationHeader,
    required this.openExternal,
    this.workspaceID,
    this.authorityEpoch,
    http.Client? client,
    String? apiBaseUrl,
    DateTime Function()? now,
  }) : _client = client ?? http.Client(),
       _ownsClient = client == null,
       _base = apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
       _now = now ?? (() => DateTime.now().toUtc()) {
    _observed = _authority();
    _watch = Timer.periodic(const Duration(seconds: 1), (_) {
      syncIdentity();
      if (!_closed &&
          _preview != null &&
          !_preview!.metadata.validUntil.isAfter(_now())) {
        _preview = null;
        error = '预约资料核验已到期，请返回刷新后重新确认。';
        notifyListeners();
      }
      if (!_closed &&
          metadata != null &&
          !metadata!.expiresAt.isAfter(_now())) {
        invalidate(notify: false);
        error = '预约资料已过期，请刷新后重试。';
        notifyListeners();
      }
    });
  }
  final String placeID;
  final String? Function() authorizationHeader;
  final String? Function()? workspaceID;
  final int Function()? authorityEpoch;
  final Future<bool> Function(Uri) openExternal;
  final http.Client _client;
  final bool _ownsClient;
  final String _base;
  final DateTime Function() _now;
  late _BookingAuthority _observed;
  Timer? _watch;
  VerifiedBookingMetadata? metadata;
  BookingPreview? _preview;
  String? error;
  String? telemetryNotice;
  bool busy = false, _closed = false;
  int _generation = 0;
  _BookingAuthority _authority() => (
    token: authorizationHeader(),
    workspace: workspaceID?.call(),
    epoch: authorityEpoch?.call() ?? 0,
  );
  bool get currentIdentity => !_closed && _observed == _authority();
  bool get canPrepare =>
      currentIdentity &&
      !busy &&
      metadata?.bookingURL != null &&
      metadata!.expiresAt.isAfter(_now());
  bool isCurrent(BookingPreview preview) =>
      currentIdentity &&
      identical(_preview, preview) &&
      preview._authority == _authority() &&
      preview._generation == _generation &&
      !busy &&
      preview.metadata.expiresAt.isAfter(_now()) &&
      preview.metadata.validUntil.isAfter(_now());
  void invalidate({bool notify = true}) {
    ++_generation;
    _preview = null;
    metadata = null;
    error = null;
    telemetryNotice = null;
    busy = false;
    _observed = _authority();
    if (!_closed && notify) notifyListeners();
  }

  void syncIdentity() {
    if (!_closed && _observed != _authority()) invalidate();
  }

  void adopt(Map<String, dynamic> data, {bool notify = true}) {
    if (_closed) return;
    ++_generation;
    _preview = null;
    busy = false;
    _observed = _authority();
    telemetryNotice = null;
    try {
      metadata = VerifiedBookingMetadata.fromJson(data, placeID, _now());
      error = null;
    } catch (_) {
      metadata = null;
      error = '预约资料缺少有效来源或已过期，暂不能打开。';
    }
    if (notify) notifyListeners();
  }

  bool _valid(_BookingAuthority authority, int generation) =>
      !_closed && generation == _generation && authority == _authority();
  Future<VerifiedBookingMetadata> _read(_BookingAuthority authority) async {
    final response = await _client
        .get(
          Uri.parse(
            '${_base.replaceFirst(RegExp(r'/$'), '')}/v1/places/${Uri.encodeComponent(placeID)}/venue',
          ),
          headers: {
            if (authority.token != null) 'Authorization': authority.token!,
          },
        )
        .timeout(const Duration(seconds: 12));
    if (response.statusCode == 404) throw const _BookingUnavailable();
    if (response.statusCode != 200 || response.bodyBytes.length > 1024 * 1024) {
      throw const FormatException();
    }
    final data =
        (jsonDecode(utf8.decode(response.bodyBytes))
                as Map<String, dynamic>)['data']
            as Map<String, dynamic>;
    return VerifiedBookingMetadata.fromJson(data, placeID, _now());
  }

  Future<BookingPreview?> prepare() async {
    syncIdentity();
    if (_closed || busy) return null;
    _preview = null;
    error = null;
    telemetryNotice = null;
    busy = true;
    final authority = _authority(), generation = ++_generation;
    notifyListeners();
    try {
      final fresh = await _read(authority);
      if (!_valid(authority, generation)) return null;
      metadata = fresh;
      if (fresh.bookingURL == null) {
        error = '未提供有效外部预约入口，请查看来源说明。';
        return null;
      }
      return _preview = BookingPreview._(fresh, authority, generation);
    } catch (e) {
      if (_valid(authority, generation)) {
        metadata = null;
        error = e is _BookingUnavailable
            ? '预约资料已不可查看，未打开旧链接。'
            : '预约资料暂不可用，请刷新后重试。';
      }
      return null;
    } finally {
      if (_valid(authority, generation)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  void cancel(BookingPreview preview) {
    if (_closed) return;
    if (identical(preview, _preview)) {
      _preview = null;
      notifyListeners();
    }
  }

  Future<bool> approve(BookingPreview preview) async {
    syncIdentity();
    if (!isCurrent(preview)) return false;
    _preview = null;
    busy = true;
    error = null;
    telemetryNotice = null;
    notifyListeners();
    try {
      final fresh = await _read(preview._authority);
      if (!_valid(preview._authority, preview._generation)) return false;
      metadata = fresh;
      if (!fresh.sameMaterial(preview.metadata) ||
          fresh.sourceRevision != preview.metadata.sourceRevision) {
        error = '预约资料已更新，请检查后重新确认。';
        return false;
      }
      if (!fresh.expiresAt.isAfter(_now()) ||
          !preview.metadata.validUntil.isAfter(_now())) {
        error = '预约资料已过期，未打开链接。';
        return false;
      }
      final opened = await openExternal(fresh.bookingURL!);
      if (_valid(preview._authority, preview._generation) && !opened) {
        error = '无法打开外部预约页面，请检查设备设置。';
      }
      if (!_valid(preview._authority, preview._generation) ||
          !opened ||
          !preview.metadata.validUntil.isAfter(_now())) {
        return false;
      }
      // A reported external open is not a booking or a provider receipt.
      // One UUID per approved open; transport uncertainty never retries or
      // opens the external page again. Borrowed HTTP client is not closed.
      try {
        await BookingAnalyticsApi(client: _client, base: _base).report(
          placeID: placeID,
          eventID: BookingAnalyticsApi.newEventID(),
          sourceVersion: preview.metadata.sourceVersion,
          validUntil: preview.metadata.validUntil,
          token: preview._authority.token,
        );
        if (_valid(preview._authority, preview._generation)) {
          telemetryNotice = '外部页面已打开；预订是否成功请在对方页面核实。';
        }
      } catch (_) {
        if (_valid(preview._authority, preview._generation)) {
          telemetryNotice = '外部页面已打开；外跳统计未确认。预订是否成功请在对方页面核实。';
        }
      }
      return _valid(preview._authority, preview._generation);
    } catch (e) {
      if (_valid(preview._authority, preview._generation)) {
        metadata = null;
        error = e is _BookingUnavailable
            ? '预约资料已不可查看，未打开旧链接。'
            : '预约资料或外部页面暂不可用，未确认预约。';
      }
      return false;
    } finally {
      if (_valid(preview._authority, preview._generation)) {
        busy = false;
        notifyListeners();
      }
    }
  }

  @override
  void dispose() {
    _closed = true;
    ++_generation;
    _preview = null;
    metadata = null;
    _watch?.cancel();
    if (_ownsClient) _client.close();
    super.dispose();
  }
}

class _BookingUnavailable implements Exception {
  const _BookingUnavailable();
}
