import 'dart:async';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'connections.dart';
import 'public_moment_api.dart';
import 'supplier_profile_api.dart';

/// One current, explicitly public human publication. It is not attendance evidence.
class PublicMomentPage extends StatefulWidget {
  const PublicMomentPage({
    super.key,
    required this.momentID,
    required this.authorizationHeader,
    this.identityChanges,
    this.workspaceID,
    this.onOpenPlace,
    this.apiBaseUrl,
    this.client,
  });
  final String momentID;
  final String? Function() authorizationHeader;
  final Listenable? identityChanges;
  final String? Function()? workspaceID;
  final ValueChanged<String>? onOpenPlace;
  final String? apiBaseUrl;
  final http.Client? client;
  @override
  State<PublicMomentPage> createState() => _PublicMomentPageState();
}

class _PublicMomentPageState extends State<PublicMomentPage> {
  late final PublicMomentApi _api = PublicMomentApi(
    authorizationHeader: () => widget.authorizationHeader(),
    apiBaseUrl: widget.apiBaseUrl,
    client: widget.client,
  );
  PublicMoment? _moment;
  String? _error;
  int _epoch = 0;
  late (String?, String?) _identity;
  (String?, String?) get _current =>
      (widget.authorizationHeader(), widget.workspaceID?.call());
  @override
  void initState() {
    super.initState();
    _identity = _current;
    widget.identityChanges?.addListener(_changed);
    unawaited(_load());
  }

  void _changed() {
    if (_identity == _current) return;
    _identity = _current;
    ++_epoch;
    setState(() {
      _moment = null;
      _error = null;
    });
    unawaited(_load());
  }

  @override
  void didUpdateWidget(PublicMomentPage old) {
    super.didUpdateWidget(old);
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_changed);
      widget.identityChanges?.addListener(_changed);
    }
    if (old.momentID != widget.momentID || _identity != _current) {
      _identity = _current;
      ++_epoch;
      _moment = null;
      unawaited(_load());
    }
  }

  Future<void> _load() async {
    final epoch = ++_epoch, identity = _current, id = widget.momentID;
    setState(() {
      _moment = null;
      _error = null;
    });
    try {
      final value = await _api.read(id);
      if (mounted &&
          epoch == _epoch &&
          identity == _current &&
          id == widget.momentID) {
        setState(() => _moment = value);
      }
    } catch (_) {
      if (mounted && epoch == _epoch && identity == _current) {
        setState(() => _error = '这条动态已撤回、不可访问，或暂时无法读取。');
      }
    }
  }

  @override
  void dispose() {
    ++_epoch;
    widget.identityChanges?.removeListener(_changed);
    _api.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('公开动态')),
    body: _error != null
        ? Center(
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(_error!),
                  TextButton(onPressed: _load, child: const Text('重新核实')),
                ],
              ),
            ),
          )
        : _moment == null
        ? const Center(child: CircularProgressIndicator())
        : RefreshIndicator(
            onRefresh: _load,
            child: ListView(
              padding: const EdgeInsets.all(24),
              children: [
                Text(
                  _moment!.title,
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
                const SizedBox(height: 12),
                Text('本人明确公开 · ${supplierDate(_moment!.publishedAt)}'),
                const SizedBox(height: 24),
                Text(_moment!.body),
                const SizedBox(height: 24),
                Text('公开地点：${_moment!.placeName}'),
                if (widget.onOpenPlace != null)
                  TextButton.icon(
                    onPressed: () => widget.onOpenPlace!(_moment!.placeID),
                    icon: const Icon(Icons.place_outlined),
                    label: const Text('查看地点'),
                  ),
                const SizedBox(height: 12),
                const Text('这是用户的公开分享，不代表已核验到访或出席。'),
                if (widget.workspaceID?.call() == null)
                  TextButton.icon(
                    onPressed: () => shareEntityToChat(
                      context,
                      authorizationHeader: widget.authorizationHeader,
                      identityChanges: widget.identityChanges,
                      workspaceID: widget.workspaceID,
                      type: 'moment',
                      id: widget.momentID,
                      apiBaseUrl: widget.apiBaseUrl,
                      client: widget.client,
                    ),
                    icon: const Icon(Icons.chat_bubble_outline),
                    label: const Text('发给好友'),
                  ),
              ],
            ),
          ),
  );
}
