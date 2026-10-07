import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

import '../config/birdtie_environment.dart';

class OrganizationMapLocationPage extends StatefulWidget {
  const OrganizationMapLocationPage({
    super.key,
    required this.organizationID,
    required this.cityID,
    required this.authorizationHeader,
  });

  final String organizationID;
  final String cityID;
  final String? Function() authorizationHeader;

  @override
  State<OrganizationMapLocationPage> createState() =>
      _OrganizationMapLocationPageState();
}

class _OrganizationMapLocationPageState
    extends State<OrganizationMapLocationPage> {
  final _latitude = TextEditingController();
  final _longitude = TextEditingController();
  final _client = http.Client();
  String _status = '读取中…';
  bool _busy = false;

  Uri get _uri => Uri.parse(
    '${BirdtieEnvironment.apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}/v1/me/organizations/'
    '${Uri.encodeComponent(widget.organizationID)}/map-location',
  );
  Map<String, String> get _headers => {
    'Authorization': ?widget.authorizationHeader(),
    'Content-Type': 'application/json',
  };

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final response = await _client.get(_uri, headers: _headers);
      if (!mounted) return;
      if (response.statusCode == 404) {
        setState(() => _status = '尚未提交公开点位。');
      } else if (response.statusCode == 200) {
        final point =
            (jsonDecode(response.body) as Map<String, dynamic>)['data']
                as Map<String, dynamic>;
        _latitude.text = '${point['latitude']}';
        _longitude.text = '${point['longitude']}';
        final visible = point['visibility'] == 'public';
        final review = point['reviewStatus'] as String;
        setState(
          () => _status = !visible
              ? '已隐藏，不在地图展示。'
              : switch (review) {
                  'approved' => '审核通过；组织身份核验通过后才会显示在地图。',
                  'rejected' => '审核未通过；修改后可重新提交。',
                  _ => '等待城市编辑审核；目前不会显示在地图。',
                },
        );
      } else {
        setState(() => _status = '读取失败，请稍后重试。');
      }
    } catch (_) {
      if (mounted) setState(() => _status = '网络不可用，请稍后重试。');
    }
  }

  Future<void> _submit() async {
    final lat = double.tryParse(_latitude.text.trim());
    final lon = double.tryParse(_longitude.text.trim());
    if (lat == null ||
        lon == null ||
        !lat.isFinite ||
        !lon.isFinite ||
        lat < -90 ||
        lat > 90 ||
        lon < -180 ||
        lon > 180) {
      setState(() => _status = '请输入有效的 WGS84 纬度和经度。');
      return;
    }
    setState(() => _busy = true);
    try {
      final response = await _client.put(
        _uri,
        headers: _headers,
        body: jsonEncode({
          'cityId': widget.cityID,
          'latitude': lat,
          'longitude': lon,
        }),
      );
      if (mounted) {
        setState(
          () => _status = response.statusCode == 200
              ? '已提交审核；审核通过且组织身份核验通过后才会公开。'
              : '提交失败，请确认管理权限和城市后重试。',
        );
      }
    } catch (_) {
      if (mounted) setState(() => _status = '网络不可用，请稍后重试。');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _hide() async {
    setState(() => _busy = true);
    try {
      final response = await _client.delete(_uri, headers: _headers);
      if (mounted) {
        setState(
          () => _status = response.statusCode == 204
              ? '已隐藏，不在地图展示。'
              : '隐藏失败，请稍后重试。',
        );
      }
    } catch (_) {
      if (mounted) setState(() => _status = '网络不可用，请稍后重试。');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    _latitude.dispose();
    _longitude.dispose();
    _client.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('组织地图位置')),
    body: ListView(
      padding: const EdgeInsets.all(24),
      children: [
        const Text('只提交组织明确授权公开的地点。请勿填写成员住址、私人位置或从活动地点推断组织坐标。'),
        const SizedBox(height: 20),
        TextField(
          controller: _latitude,
          decoration: const InputDecoration(labelText: '纬度（WGS84）'),
          keyboardType: const TextInputType.numberWithOptions(
            decimal: true,
            signed: true,
          ),
        ),
        TextField(
          controller: _longitude,
          decoration: const InputDecoration(labelText: '经度（WGS84）'),
          keyboardType: const TextInputType.numberWithOptions(
            decimal: true,
            signed: true,
          ),
        ),
        const SizedBox(height: 16),
        Text(_status),
        const SizedBox(height: 16),
        FilledButton(
          onPressed: _busy ? null : _submit,
          child: const Text('提交公开位置审核'),
        ),
        TextButton(
          onPressed: _busy ? null : _hide,
          child: const Text('隐藏地图位置'),
        ),
      ],
    ),
  );
}
