import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../auth/birdtie_auth_controller.dart';
import '../config/birdtie_environment.dart';
import 'opportunity_reasons.dart';

/// Matching is an explicit owner action. Candidates never become map entities.
class NewPeoplePage extends StatefulWidget {
  const NewPeoplePage({
    super.key,
    required this.auth,
    this.apiBaseUrl,
    this.client,
    this.workspaceChanges,
    this.organizationWorkspaceID,
    this.initialSourceIntentID,
  });

  final BirdtieAuthController auth;
  final String? apiBaseUrl;
  final http.Client? client;
  final Listenable? workspaceChanges;
  final String? Function()? organizationWorkspaceID;
  final String? initialSourceIntentID;

  @override
  State<NewPeoplePage> createState() => _NewPeoplePageState();
}

class _NewPeoplePageState extends State<NewPeoplePage> {
  late http.Client _client = widget.client ?? http.Client();
  late bool _ownsClient = widget.client == null;
  late (String?, String?, String?) _identity;
  bool _initialSelectionAllowed = true;
  final _title = TextEditingController();
  final _category = TextEditingController();
  final _area = TextEditingController();
  final _platform = TextEditingController();
  final _min = TextEditingController();
  final _max = TextEditingController();
  String? _error, _formError, _sourceId, _cityId, _placeId;
  String _modality = 'ONLINE';
  bool? _enabled;
  bool _busy = false, _searching = false, _searched = false;
  bool _truncated = false, _placesLoading = false;
  String? _placesError;
  int _serial = 0, _locationSerial = 0, _expiryDays = 7;
  BuildContext? _dialogContext;
  List<Map<String, dynamic>> _intents = [], _cities = [], _places = [];
  List<_Candidate> _candidates = [];
  final _cityNames = <String, String>{};
  final _placeNames = <String, String>{};

  String get _base => widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
  (String?, String?, String?) get _currentIdentity => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.organizationWorkspaceID?.call(),
  );
  String? get _personalToken =>
      _currentIdentity.$3 == null ? widget.auth.authorizationHeader : null;
  Uri _url(String path) =>
      Uri.parse('${_base.replaceFirst(RegExp(r'/$'), '')}/v1/$path');
  bool _current(int serial, String token) =>
      mounted &&
      serial == _serial &&
      _identity == _currentIdentity &&
      _personalToken == token;
  Map<String, String> _headers(String token, {bool json = false}) => {
    'Authorization': token,
    if (json) 'Content-Type': 'application/json',
  };
  Object? _data(http.Response response, {int status = 200}) {
    if (response.statusCode != status) {
      throw _RequestFailure(response.statusCode);
    }
    return (jsonDecode(utf8.decode(response.bodyBytes))
        as Map<String, dynamic>)['data'];
  }

  @override
  void initState() {
    super.initState();
    _identity = _currentIdentity;
    widget.auth.addListener(_authChanged);
    widget.workspaceChanges?.addListener(_authChanged);
    unawaited(_load());
  }

  @override
  void didUpdateWidget(covariant NewPeoplePage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.auth != widget.auth) {
      oldWidget.auth.removeListener(_authChanged);
      widget.auth.addListener(_authChanged);
    }
    if (oldWidget.workspaceChanges != widget.workspaceChanges) {
      oldWidget.workspaceChanges?.removeListener(_authChanged);
      widget.workspaceChanges?.addListener(_authChanged);
    }
    final transportChanged =
        oldWidget.client != widget.client ||
        oldWidget.apiBaseUrl != widget.apiBaseUrl;
    if (transportChanged) {
      if (_ownsClient) _client.close();
      _client = widget.client ?? http.Client();
      _ownsClient = widget.client == null;
    }
    if (transportChanged ||
        oldWidget.auth != widget.auth ||
        oldWidget.workspaceChanges != widget.workspaceChanges ||
        oldWidget.organizationWorkspaceID != widget.organizationWorkspaceID ||
        oldWidget.initialSourceIntentID != widget.initialSourceIntentID) {
      _resetIdentity(
        allowInitial:
            oldWidget.initialSourceIntentID != widget.initialSourceIntentID &&
            _identity == _currentIdentity,
      );
    } else {
      _authChanged();
    }
  }

  void _closeDialog() {
    final dialog = _dialogContext;
    _dialogContext = null;
    if (dialog != null &&
        dialog.mounted &&
        ModalRoute.of(dialog)?.isCurrent == true) {
      Navigator.of(dialog).pop();
    }
  }

  void _clearCandidates() {
    _candidates = [];
    _searched = false;
    _searching = false;
    _truncated = false;
  }

  void _authChanged() {
    if (_identity == _currentIdentity) return;
    _resetIdentity();
  }

  void _resetIdentity({bool allowInitial = false}) {
    final token = _personalToken;
    _identity = _currentIdentity;
    _initialSelectionAllowed = allowInitial;
    ++_serial;
    ++_locationSerial;
    _closeDialog();
    for (final controller in [
      _title,
      _category,
      _area,
      _platform,
      _min,
      _max,
    ]) {
      controller.clear();
    }
    setState(() {
      _enabled = null;
      _intents = [];
      _cities = [];
      _places = [];
      _cityNames.clear();
      _placeNames.clear();
      _sourceId = _cityId = _placeId = null;
      _error = _formError = _placesError = null;
      _busy = _placesLoading = false;
      _modality = 'ONLINE';
      _expiryDays = 7;
      _clearCandidates();
    });
    if (token != null) unawaited(_load());
  }

  @override
  void dispose() {
    ++_serial;
    ++_locationSerial;
    widget.auth.removeListener(_authChanged);
    widget.workspaceChanges?.removeListener(_authChanged);
    for (final controller in [
      _title,
      _category,
      _area,
      _platform,
      _min,
      _max,
    ]) {
      controller.dispose();
    }
    if (_ownsClient) _client.close();
    super.dispose();
  }

  Future<void> _load({bool? save}) async {
    final token = _personalToken;
    if (_busy || token == null || _base.isEmpty) return;
    final serial = ++_serial;
    setState(() {
      _busy = true;
      _error = null;
      _clearCandidates();
      if (save == false) _enabled = false;
    });
    try {
      final consent =
          _data(
                await (save == null
                        ? _client.get(
                            _url('me/new-people/consent'),
                            headers: _headers(token),
                          )
                        : _client.put(
                            _url('me/new-people/consent'),
                            headers: _headers(token, json: true),
                            body: jsonEncode({'enabled': save}),
                          ))
                    .timeout(const Duration(seconds: 12)),
              )
              as Map<String, dynamic>;
      if (!_current(serial, token)) return;
      setState(() => _enabled = consent['enabled'] as bool);
      final rows =
          _data(
                await _client
                    .get(
                      _url('me/new-people/intents'),
                      headers: _headers(token),
                    )
                    .timeout(const Duration(seconds: 12)),
              )
              as List<dynamic>;
      if (!_current(serial, token)) return;
      setState(() {
        _intents = rows.cast<Map<String, dynamic>>();
        if (!_activeIntents.any((row) => row['id'] == _sourceId)) {
          _sourceId = null;
        }
        if (_initialSelectionAllowed) {
          _initialSelectionAllowed = false;
          final initial = widget.initialSourceIntentID;
          if (_enabled == true &&
              initial != null &&
              _activeIntents.any(
                (row) => row['id'] == initial && row['audience'] == 'PUBLIC',
              )) {
            _sourceId = initial;
          }
        }
      });
    } catch (_) {
      if (_current(serial, token)) {
        setState(
          () => _error = save == null
              ? '设置或意图暂不可用，请刷新重试。'
              : '授权设置尚未确认，请刷新核对。当前候选已隐藏。',
        );
      }
    } finally {
      if (_current(serial, token)) setState(() => _busy = false);
    }
  }

  List<Map<String, dynamic>> get _activeIntents => _intents.where((row) {
    final expires = DateTime.tryParse(row['expiresAt'] as String? ?? '');
    return row['creatorAccountId'] == widget.auth.accountID &&
        row['status'] == 'ACTIVE' &&
        row['type'] == 'FIND_COMPANION' &&
        row['audience'] != 'PRIVATE' &&
        expires != null &&
        expires.isAfter(DateTime.now());
  }).toList();

  Future<void> _loadLocations({String? city}) async {
    final token = _personalToken;
    if (token == null || _base.isEmpty || _modality == 'ONLINE') return;
    final serial = ++_locationSerial;
    setState(() {
      _placesLoading = true;
      _placesError = null;
      if (city != null) _places = [];
    });
    bool current() =>
        mounted &&
        serial == _locationSerial &&
        _identity == _currentIdentity &&
        _personalToken == token &&
        _modality != 'ONLINE';
    try {
      final rows =
          _data(
                await _client
                    .get(
                      city == null
                          ? _url('cities')
                          : _url('cities/${Uri.encodeComponent(city)}/places'),
                      headers: _headers(token),
                    )
                    .timeout(const Duration(seconds: 12)),
              )
              as List<dynamic>;
      if (!current()) return;
      setState(() {
        if (city == null) {
          _cities = rows.cast<Map<String, dynamic>>();
          for (final row in _cities) {
            _cityNames[row['id'] as String] = row['name'] as String;
          }
        } else {
          _places = rows.cast<Map<String, dynamic>>();
          for (final row in _places) {
            _placeNames[row['id'] as String] = row['name'] as String;
          }
        }
      });
    } catch (_) {
      if (current()) setState(() => _placesError = '城市或公开地点读取失败，请重试。');
    } finally {
      if (current()) setState(() => _placesLoading = false);
    }
  }

  void _changeModality(String value) {
    ++_locationSerial;
    setState(() {
      _modality = value;
      _cityId = _placeId = null;
      _places = [];
      _area.clear();
      _platform.clear();
      _placesError = null;
      _placesLoading = false;
      _formError = null;
    });
    if (value != 'ONLINE') unawaited(_loadLocations());
  }

  Future<void> _saveDraft() async {
    final token = _personalToken;
    if (_busy || token == null || _enabled != true) return;
    final min = int.tryParse(_min.text.trim());
    final max = int.tryParse(_max.text.trim());
    final invalidCount =
        (_min.text.trim().isNotEmpty &&
            (min == null || min < 1 || min > 100)) ||
        (_max.text.trim().isNotEmpty &&
            (max == null || max < 1 || max > 100)) ||
        (min != null && max != null && min > max);
    if (_title.text.trim().isEmpty ||
        _category.text.trim().isEmpty ||
        invalidCount ||
        (_modality != 'ONLINE' &&
            (_cityId == null ||
                (_placeId == null && _area.text.trim().isEmpty))) ||
        (_modality == 'HYBRID' && _platform.text.trim().isEmpty)) {
      setState(() => _formError = '请填写想做的事、类别和参与方式所需的范围；人数为 1–100，最多不少于最少。');
      return;
    }
    final serial = ++_serial;
    final input = {
      'title': _title.text.trim(),
      'category': _category.text.trim(),
      'modality': _modality,
      if (_modality != 'ONLINE') 'cityId': _cityId,
      if (_modality != 'ONLINE' && _placeId != null) 'placeId': _placeId,
      if (_modality != 'ONLINE' && _placeId == null)
        'areaLabel': _area.text.trim(),
      if (_modality != 'IN_PERSON' && _platform.text.trim().isNotEmpty)
        'onlinePlatform': _platform.text.trim(),
      'minParticipants': ?min,
      'maxParticipants': ?max,
      'expiresAt': DateTime.now()
          .toUtc()
          .add(Duration(days: _expiryDays))
          .toIso8601String(),
    };
    setState(() {
      _busy = true;
      _formError = null;
      _error = null;
      _clearCandidates();
    });
    try {
      _data(
        await _client
            .post(
              _url('me/new-people/intents'),
              headers: _headers(token, json: true),
              body: jsonEncode(input),
            )
            .timeout(const Duration(seconds: 12)),
        status: 201,
      );
      if (!_current(serial, token)) return;
      for (final controller in [
        _title,
        _category,
        _area,
        _platform,
        _min,
        _max,
      ]) {
        controller.clear();
      }
      setState(() => _busy = false);
      await _load();
      if (mounted && _current(serial, token)) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('已保存草稿。请预览并确认公开，草稿不会参与匹配。')),
        );
      }
    } catch (_) {
      if (_current(serial, token)) {
        setState(() => _formError = '草稿保存失败，请检查输入和网络后重试。');
      }
    } finally {
      if (_current(serial, token)) setState(() => _busy = false);
    }
  }

  String _mode(String? value) => switch (value) {
    'IN_PERSON' => '线下',
    'HYBRID' => '线下＋线上',
    _ => '线上',
  };
  String _state(String? value) => switch (value) {
    'ACTIVE' => '已激活 · 按受众参与匹配',
    'CANCELLED' => '已取消',
    'EXPIRED' => '已过期',
    'MATCHED' => '已匹配',
    'CONVERTED' => '已转化',
    _ => '草稿 · 仅自己可见',
  };

  Future<void> _transition(
    Map<String, dynamic> row, {
    required bool publish,
  }) async {
    final token = _personalToken;
    if (_busy || token == null) return;
    final serial = ++_serial;
    setState(() {
      _error = null;
      _clearCandidates();
    });
    final constraints = row['constraints'] as Map<String, dynamic>? ?? {};
    String? cityName, placeName;
    if (publish && row['modality'] != 'ONLINE') {
      setState(() {
        _busy = true;
        _error = null;
        _clearCandidates();
      });
      try {
        final cityId = row['cityId'] as String?;
        if (cityId == null || cityId.isEmpty) {
          throw const FormatException('missing physical scope');
        }
        cityName = _cityNames[cityId];
        if (cityName == null) {
          final city =
              _data(
                    await _client
                        .get(
                          _url('cities/${Uri.encodeComponent(cityId)}'),
                          headers: _headers(token),
                        )
                        .timeout(const Duration(seconds: 12)),
                  )
                  as Map<String, dynamic>;
          cityName = city['name'] as String;
          if (cityName.trim().isEmpty) {
            throw const FormatException('missing city name');
          }
          if (!_current(serial, token)) return;
          _cityNames[cityId] = cityName;
        }
        final placeId = constraints['placeId'] as String?;
        if (placeId != null && placeId.isNotEmpty) {
          placeName = _placeNames[placeId];
          if (placeName == null) {
            final place =
                _data(
                      await _client
                          .get(
                            _url('places/${Uri.encodeComponent(placeId)}'),
                            headers: _headers(token),
                          )
                          .timeout(const Duration(seconds: 12)),
                    )
                    as Map<String, dynamic>;
            placeName = place['name'] as String;
            if (placeName.trim().isEmpty) {
              throw const FormatException('missing place name');
            }
            if (!_current(serial, token)) return;
            _placeNames[placeId] = placeName;
          }
        }
      } catch (_) {
        if (_current(serial, token)) {
          setState(() => _error = '城市或地点暂不可核对，尚未公开。请刷新后重试。');
        }
        return;
      } finally {
        if (_current(serial, token)) setState(() => _busy = false);
      }
    }
    if (!mounted || !_current(serial, token)) return;
    final confirmed = await showDialog<bool>(
      context: context,
      useRootNavigator: false,
      builder: (dialog) {
        _dialogContext = dialog;
        return AlertDialog(
          title: Text(publish ? '预览并确认公开' : '取消这条找伙伴意图？'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(row['title'] as String? ?? ''),
                if (publish) ...[
                  Text(
                    '类别：${constraints['category'] ?? ''} · ${_mode(row['modality'] as String?)}',
                  ),
                  if (cityName != null) Text('活动城市：$cityName'),
                  if (placeName != null) Text('公开地点：$placeName'),
                  for (final field in [
                    'areaLabel',
                    'onlinePlatform',
                    'minParticipants',
                    'maxParticipants',
                  ])
                    if (constraints[field] != null)
                      Text('${_constraintLabel(field)}：${constraints[field]}'),
                  Text('到期：${_expiry(row['expiresAt'] as String?)}'),
                  const SizedBox(height: 12),
                  const Text(
                    '确认后，意图将公开给有权查看的人，并在双方开启选择时用于规则式匹配。需要公开个人资料；不代表身份已经核验。不会自动发送申请、成为好友或开启聊天。',
                  ),
                ] else
                  const Text('取消后停止使用这条意图发现和邀请。已经主动发送的好友申请仍可在收件箱处理。'),
              ],
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialog, false),
              child: const Text('返回'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(dialog, true),
              child: Text(publish ? '确认公开' : '确认取消'),
            ),
          ],
        );
      },
    );
    _dialogContext = null;
    if (confirmed != true ||
        !_current(serial, token) ||
        (publish && _enabled != true)) {
      return;
    }
    final requestSerial = ++_serial;
    setState(() {
      _busy = true;
      _error = null;
      _clearCandidates();
    });
    try {
      _data(
        await _client
            .post(
              _url(
                'me/social-intents/${Uri.encodeComponent(row['id'] as String)}/${publish ? 'activate' : 'cancel'}',
              ),
              headers: _headers(token, json: true),
              body: jsonEncode({'confirmed': true}),
            )
            .timeout(const Duration(seconds: 12)),
      );
      if (!_current(requestSerial, token)) return;
      setState(() => _busy = false);
      await _load();
    } catch (_) {
      if (_current(requestSerial, token)) {
        setState(
          () => _error = publish
              ? '公开失败。请确认个人资料已公开，且意图未过期；刷新后重试。'
              : '取消尚未确认，请刷新核对当前状态。',
        );
      }
    } finally {
      if (_current(requestSerial, token)) setState(() => _busy = false);
    }
  }

  String _constraintLabel(String value) => switch (value) {
    'areaLabel' => '大致区域',
    'onlinePlatform' => '线上平台',
    'minParticipants' => '最少人数',
    _ => '最多人数',
  };
  String _expiry(String? value) {
    final date = DateTime.tryParse(value ?? '')?.toLocal();
    return date == null ? '未提供' : '${date.year}年${date.month}月${date.day}日';
  }

  void _changeSource(String? value) {
    ++_serial;
    _closeDialog();
    setState(() {
      _sourceId = value;
      _error = null;
      _clearCandidates();
    });
  }

  Future<void> _search() async {
    final token = _personalToken;
    final source = _sourceId;
    if (_busy ||
        _searching ||
        _enabled != true ||
        token == null ||
        source == null ||
        !_activeIntents.any((row) => row['id'] == source)) {
      return;
    }
    final serial = ++_serial;
    setState(() {
      _searching = true;
      _searched = false;
      _error = null;
      _candidates = [];
      _truncated = false;
    });
    try {
      final result =
          _data(
                await _client
                    .get(
                      _url(
                        'me/new-people/candidates',
                      ).replace(queryParameters: {'sourceIntentId': source}),
                      headers: _headers(token),
                    )
                    .timeout(const Duration(seconds: 12)),
              )
              as Map<String, dynamic>;
      if (!_current(serial, token) ||
          _sourceId != source ||
          _enabled != true ||
          !_activeIntents.any((row) => row['id'] == source)) {
        return;
      }
      if (result['source'] != 'RULE_BASED' ||
          result['ruleVersion'] != 'v1' ||
          result['sourceIntentId'] != source) {
        throw const FormatException('source mismatch');
      }
      final candidates = (result['candidates'] as List<dynamic>)
          .map((row) => _Candidate.fromJson(row as Map<String, dynamic>))
          .toList();
      if (candidates.any((row) => row.sourceId != source)) {
        throw const FormatException('candidate source mismatch');
      }
      setState(() {
        _candidates = candidates;
        _truncated = result['truncated'] as bool? ?? false;
        _searched = true;
      });
    } catch (error) {
      if (_current(serial, token)) {
        setState(
          () => _error =
              error is _RequestFailure &&
                  (error.status == 404 ||
                      error.status == 409 ||
                      error.status == 403)
              ? '来源或授权已变化，请刷新意图与设置后重新选择。'
              : '同行者暂不可用，请重试。',
        );
      }
    } finally {
      if (_current(serial, token)) setState(() => _searching = false);
    }
  }

  Future<void> _invite(_Candidate candidate) async {
    final token = _personalToken;
    if (_busy ||
        token == null ||
        _enabled != true ||
        candidate.sourceId != _sourceId ||
        !_activeIntents.any((row) => row['id'] == candidate.sourceId)) {
      return;
    }
    final serial = _serial;
    final note = await showDialog<String>(
      context: context,
      useRootNavigator: false,
      builder: (dialog) {
        _dialogContext = dialog;
        return _InvitationDialog(name: candidate.name);
      },
    );
    _dialogContext = null;
    if (note == null ||
        !_current(serial, token) ||
        _enabled != true ||
        candidate.sourceId != _sourceId ||
        !_activeIntents.any((row) => row['id'] == candidate.sourceId)) {
      return;
    }
    final requestSerial = ++_serial;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      _data(
        await _client
            .post(
              _url('me/new-people/invitations'),
              headers: _headers(token, json: true),
              body: jsonEncode({
                'sourceIntentId': candidate.sourceId,
                'candidateIntentId': candidate.intentId,
                'note': note,
                'confirmed': true,
              }),
            )
            .timeout(const Duration(seconds: 12)),
        status: 201,
      );
      if (!mounted || !_current(requestSerial, token)) return;
      setState(
        () => _candidates.removeWhere(
          (row) => row.accountId == candidate.accountId,
        ),
      );
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('好友申请已发送。对方接受后才建立关系；请到收件箱查看回复。')),
      );
    } catch (error) {
      if (_current(requestSerial, token)) {
        setState(() {
          _clearCandidates();
          _error =
              error is _RequestFailure &&
                  (error.status == 404 ||
                      error.status == 409 ||
                      error.status == 403)
              ? '来源、授权或申请状态已变化，没有确认发送成功。请刷新后重新查看。'
              : '申请发送结果尚未确认，请先查看收件箱，避免重复发送。';
        });
      }
    } finally {
      if (_current(requestSerial, token)) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(
      title: const Text('找新朋友'),
      actions: [
        IconButton(
          onPressed: _busy ? null : _load,
          tooltip: '刷新意图与设置',
          icon: const Icon(Icons.refresh),
        ),
      ],
    ),
    body: widget.organizationWorkspaceID?.call() != null
        ? const Center(child: Text('请切回个人身份管理找伙伴意图；组织身份不能沿用此批准。'))
        : widget.auth.authorizationHeader == null
        ? const Center(child: Text('登录后管理本人的找伙伴意图。'))
        : _base.isEmpty
        ? const Center(child: Text('请连接 Birdtie 后发现同行者。'))
        : ListView(
            padding: const EdgeInsets.all(20),
            children: [
              const Text(
                '从一件想一起做的事开始',
                style: TextStyle(fontSize: 22, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 8),
              const Text(
                '默认关闭。仅使用双方主动公开或有权查看的找伙伴意图；不读取私聊、私密记忆、学校、身份或定位。你可以随时关闭。',
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('允许用公开意图发现新朋友'),
                value: _enabled ?? false,
                onChanged: _busy || _enabled == null
                    ? null
                    : (value) => _load(save: value),
              ),
              if (_busy) const LinearProgressIndicator(),
              if (_error != null)
                TextButton(
                  onPressed: _busy ? null : _load,
                  child: Text(_error!),
                ),
              if (_enabled == false) const Text('尚未开启，不查询候选或发送新朋友邀请。'),
              if (_enabled == true) ...[
                const Divider(height: 28),
                Text(
                  '记下想一起做的事',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                const Text('先保存为仅自己可见的草稿，再单独预览并确认公开。公开资料与匹配选择不会自动开启聊天。'),
                TextField(
                  key: const Key('new_people_title'),
                  controller: _title,
                  enabled: !_busy,
                  maxLength: 160,
                  decoration: const InputDecoration(
                    labelText: '想做什么',
                    hintText: '例如：找伙伴讨论羽毛球',
                  ),
                ),
                TextField(
                  key: const Key('new_people_category'),
                  controller: _category,
                  enabled: !_busy,
                  maxLength: 80,
                  decoration: const InputDecoration(
                    labelText: '活动类别',
                    hintText: '例如：羽毛球',
                  ),
                ),
                DropdownButtonFormField<String>(
                  key: ValueKey('modality_$_modality'),
                  initialValue: _modality,
                  decoration: const InputDecoration(labelText: '参与方式'),
                  items: const [
                    DropdownMenuItem(value: 'ONLINE', child: Text('线上')),
                    DropdownMenuItem(value: 'IN_PERSON', child: Text('线下')),
                    DropdownMenuItem(value: 'HYBRID', child: Text('线下＋线上')),
                  ],
                  onChanged: _busy
                      ? null
                      : (value) {
                          if (value != null) _changeModality(value);
                        },
                ),
                if (_modality != 'ONLINE') ...[
                  const Text('请明确选择活动范围。这不声明你居住在哪里，也不提供地图位置。'),
                  if (_placesLoading) const LinearProgressIndicator(),
                  if (_placesError != null)
                    TextButton(
                      onPressed: () => _loadLocations(city: _cityId),
                      child: Text(_placesError!),
                    ),
                  DropdownButtonFormField<String>(
                    key: ValueKey('city_$_cityId'),
                    initialValue: _cityId,
                    decoration: const InputDecoration(labelText: '活动城市'),
                    items: [
                      for (final city in _cities)
                        DropdownMenuItem(
                          value: city['id'] as String,
                          child: Text(city['name'] as String),
                        ),
                    ],
                    onChanged: _busy
                        ? null
                        : (value) {
                            setState(() {
                              _cityId = value;
                              _placeId = null;
                              _places = [];
                            });
                            if (value != null) {
                              unawaited(_loadLocations(city: value));
                            }
                          },
                  ),
                  DropdownButtonFormField<String>(
                    key: ValueKey('place_$_cityId/${_placeId ?? ''}'),
                    initialValue: _placeId ?? '',
                    decoration: const InputDecoration(labelText: '公开地点（可选）'),
                    items: [
                      const DropdownMenuItem(value: '', child: Text('使用大致区域')),
                      for (final place in _places)
                        DropdownMenuItem(
                          value: place['id'] as String,
                          child: Text(place['name'] as String),
                        ),
                    ],
                    onChanged: _busy || _placesLoading
                        ? null
                        : (value) => setState(
                            () => _placeId = value == '' ? null : value,
                          ),
                  ),
                  if (_placeId == null)
                    TextField(
                      key: const Key('new_people_area'),
                      controller: _area,
                      enabled: !_busy,
                      maxLength: 160,
                      decoration: const InputDecoration(
                        labelText: '大致区域',
                        hintText: '例如：市中心；请勿填写住址',
                      ),
                    ),
                ],
                if (_modality != 'IN_PERSON')
                  TextField(
                    key: const Key('new_people_platform'),
                    controller: _platform,
                    enabled: !_busy,
                    maxLength: 80,
                    decoration: InputDecoration(
                      labelText: _modality == 'HYBRID' ? '线上平台' : '线上平台（可选）',
                      hintText: '只填写平台名称，不填写联系方式或会议链接',
                    ),
                  ),
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        key: const Key('new_people_min'),
                        controller: _min,
                        enabled: !_busy,
                        maxLength: 3,
                        keyboardType: TextInputType.number,
                        decoration: const InputDecoration(
                          labelText: '最少人数（可选）',
                        ),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: TextField(
                        key: const Key('new_people_max'),
                        controller: _max,
                        enabled: !_busy,
                        maxLength: 3,
                        keyboardType: TextInputType.number,
                        decoration: const InputDecoration(
                          labelText: '最多人数（可选）',
                        ),
                      ),
                    ),
                  ],
                ),
                DropdownButtonFormField<int>(
                  initialValue: _expiryDays,
                  decoration: const InputDecoration(
                    labelText: '意图有效期（不代表活动时间）',
                  ),
                  items: const [
                    DropdownMenuItem(value: 1, child: Text('1 天')),
                    DropdownMenuItem(value: 7, child: Text('7 天')),
                    DropdownMenuItem(value: 30, child: Text('30 天')),
                  ],
                  onChanged: _busy
                      ? null
                      : (value) => setState(() => _expiryDays = value ?? 7),
                ),
                if (_formError != null)
                  Text(
                    _formError!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: _busy ? null : _saveDraft,
                  child: const Text('保存找伙伴草稿'),
                ),
              ],
              const Divider(height: 28),
              Text('我的找伙伴意图', style: Theme.of(context).textTheme.titleMedium),
              if (!_busy && _intents.isEmpty) const Text('还没有找伙伴意图。'),
              for (final row in _intents)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 8),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(row['title'] as String? ?? '找伙伴意图'),
                      Text(
                        '${_mode(row['modality'] as String?)} · ${_state(row['status'] as String?)}',
                      ),
                      Text('到期：${_expiry(row['expiresAt'] as String?)}'),
                      Wrap(
                        spacing: 8,
                        children: [
                          if (row['status'] == 'DRAFT' &&
                              row['audience'] == 'PUBLIC')
                            TextButton(
                              onPressed: _busy || _enabled != true
                                  ? null
                                  : () => _transition(row, publish: true),
                              child: const Text('预览并公开'),
                            ),
                          if ([
                            'DRAFT',
                            'ACTIVE',
                            'MATCHED',
                          ].contains(row['status']))
                            TextButton(
                              onPressed: _busy
                                  ? null
                                  : () => _transition(row, publish: false),
                              child: const Text('取消意图'),
                            ),
                        ],
                      ),
                    ],
                  ),
                ),
              if (_enabled == true) ...[
                const Divider(height: 28),
                Text('主动查看同行者', style: Theme.of(context).textTheme.titleMedium),
                const Text('规则只比较明确填写的类别、参与方式和范围，不推断亲密度、身份或空闲时间。'),
                if (_activeIntents.isEmpty)
                  const Text('先公开一条有效的找伙伴意图，再主动查看候选。'),
                DropdownButtonFormField<String>(
                  key: ValueKey('source_$_sourceId'),
                  initialValue: _sourceId,
                  decoration: const InputDecoration(labelText: '选择本次使用的有效意图'),
                  isExpanded: true,
                  items: [
                    for (final row in _activeIntents)
                      DropdownMenuItem(
                        value: row['id'] as String,
                        child: Text(
                          row['title'] as String,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                  ],
                  onChanged: _busy ? null : _changeSource,
                ),
                FilledButton(
                  onPressed: _busy || _searching || _sourceId == null
                      ? null
                      : _search,
                  child: Text(_searching ? '正在查看…' : '查看合适的同行者'),
                ),
                if (_searching) const LinearProgressIndicator(),
                if (_searched && _candidates.isEmpty)
                  const Text('暂无符合当前条件的同行者。可以稍后再试；没有自动发送申请。'),
                if (_truncated) const Text('当前最多展示 50 位同行者。'),
                for (final candidate in _candidates)
                  Padding(
                    padding: const EdgeInsets.only(top: 20),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          candidate.name,
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        Text(
                          '${candidate.category} · ${_mode(candidate.modality)}',
                        ),
                        for (final reason in candidate.reasons) Text(reason),
                        const Text('来源：双方主动开放的找伙伴意图 · 规则匹配'),
                        OutlinedButton(
                          onPressed: _busy ? null : () => _invite(candidate),
                          child: const Text('发送好友申请'),
                        ),
                      ],
                    ),
                  ),
              ],
            ],
          ),
  );
}

class _Candidate {
  const _Candidate(
    this.sourceId,
    this.intentId,
    this.accountId,
    this.name,
    this.category,
    this.modality,
    this.reasons,
  );
  final String sourceId, intentId, accountId, name, category, modality;
  final List<String> reasons;
  factory _Candidate.fromJson(Map<String, dynamic> row) => _Candidate(
    row['sourceIntentId'] as String,
    row['candidateIntentId'] as String,
    row['accountId'] as String,
    row['displayName'] as String,
    row['category'] as String,
    row['modality'] as String,
    projectOpportunityReasons(
      (row['reasonCodes'] as List<dynamic>? ?? []).cast<String>(),
      scheme: OpportunityReasonScheme.newPeopleV1,
    ),
  );
}

class _RequestFailure implements Exception {
  const _RequestFailure(this.status);
  final int status;
}

class _InvitationDialog extends StatefulWidget {
  const _InvitationDialog({required this.name});
  final String name;
  @override
  State<_InvitationDialog> createState() => _InvitationDialogState();
}

class _InvitationDialogState extends State<_InvitationDialog> {
  final _note = TextEditingController();
  bool _invalid = false;
  @override
  void dispose() {
    _note.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('向 ${widget.name} 发送好友申请？'),
    content: SingleChildScrollView(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Text('你的显示名称和这段留言会分享给对方。对方可以接受或拒绝；发送不会自动成为好友或开启聊天。'),
          TextField(
            key: const Key('new_people_note'),
            controller: _note,
            maxLength: 280,
            maxLines: 3,
            decoration: InputDecoration(
              labelText: '介绍一下想一起做的事',
              errorText: _invalid ? '请先填写留言。' : null,
            ),
          ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(
        onPressed: () {
          final note = _note.text.trim();
          if (note.isEmpty) {
            setState(() => _invalid = true);
            return;
          }
          Navigator.pop(context, note);
        },
        child: const Text('确认发送好友申请'),
      ),
    ],
  );
}
