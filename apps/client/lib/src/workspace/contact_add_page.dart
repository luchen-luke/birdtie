import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../app/birdtie_app_bar.dart';
import '../auth/account_access_notice.dart';
import '../auth/birdtie_auth_controller.dart';
import '../config/birdtie_environment.dart';
import 'contact_code.dart';
import 'now_ui_tokens.dart';
import 'public_person_page.dart';

abstract class _ContactPage extends StatefulWidget {
  const _ContactPage({
    super.key,
    required this.auth,
    this.identityChanges,
    this.workspaceID,
    this.client,
    this.apiBaseUrl,
    this.onSignIn,
    this.onUnauthorized,
    this.onOpenPerson,
    this.embedded = false,
  });
  final BirdtieAuthController auth;
  final Listenable? identityChanges;
  final String? Function()? workspaceID;
  final http.Client? client;
  final String? apiBaseUrl;
  final Future<void> Function()? onSignIn;
  final Future<void> Function(String rejectedToken)? onUnauthorized;
  final Future<void> Function(String accountID)? onOpenPerson;
  final bool embedded;
}

abstract class _ContactState<W extends _ContactPage> extends State<W> {
  late http.Client client;
  late bool _ownsClient;
  late (String?, String?, String?) _identity;
  int epoch = 0;
  bool _personOpening = false;
  bool _sourceRetired = false;
  (String?, String?, String?) get identity => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    widget.workspaceID?.call(),
  );
  bool get personal =>
      !_sourceRetired &&
      widget.auth.signedIn &&
      identity.$1 != null &&
      identity.$2 != null &&
      identity.$3 == null;
  Listenable get changes =>
      Listenable.merge([widget.auth, widget.identityChanges]);
  bool current(int version, (String?, String?, String?) owner) =>
      mounted && epoch == version && identity == owner && personal;

  @override
  void initState() {
    super.initState();
    client = widget.client ?? http.Client();
    _ownsClient = widget.client == null;
    _identity = identity;
    widget.auth.addListener(_changed);
    widget.identityChanges?.addListener(_boundaryChanged);
  }

  void retireIdentity(bool accountChanged) {}

  void _changed() {
    if (!mounted) return;
    final next = identity;
    if (next != _identity) {
      final accountChanged = next.$2 != _identity.$2;
      _identity = next;
      ++epoch;
      retireIdentity(accountChanged);
    }
    setState(() {});
  }

  void _boundaryChanged() {
    if (!mounted) return;
    // True token/account/workspace changes keep the existing sign-in recovery.
    if (identity != _identity) {
      _changed();
      return;
    }
    if (_sourceRetired) return;
    // Same actor with a new provider/source cannot keep using this route's
    // captured client or revive its old private profile/approval after A-B-A.
    _sourceRetired = true;
    ++epoch;
    retireIdentity(false);
    setState(() {});
  }

  @override
  void didUpdateWidget(covariant W old) {
    super.didUpdateWidget(old);
    final rebound =
        old.auth != widget.auth ||
        old.identityChanges != widget.identityChanges ||
        old.workspaceID != widget.workspaceID ||
        old.client != widget.client ||
        old.apiBaseUrl != widget.apiBaseUrl ||
        old.onOpenPerson != widget.onOpenPerson;
    if (old.auth != widget.auth) {
      old.auth.removeListener(_changed);
      widget.auth.addListener(_changed);
    }
    if (old.identityChanges != widget.identityChanges) {
      old.identityChanges?.removeListener(_boundaryChanged);
      widget.identityChanges?.addListener(_boundaryChanged);
    }
    if (old.client != widget.client) {
      if (_ownsClient) client.close();
      client = widget.client ?? http.Client();
      _ownsClient = widget.client == null;
    }
    if (rebound || identity != _identity) {
      final accountChanged = identity.$2 != _identity.$2;
      _identity = identity;
      ++epoch;
      retireIdentity(accountChanged);
    }
  }

  @override
  void dispose() {
    ++epoch;
    widget.auth.removeListener(_changed);
    widget.identityChanges?.removeListener(_boundaryChanged);
    if (_ownsClient) client.close();
    super.dispose();
  }

  Widget page(String title, Widget child) => widget.embedded
      ? child
      : Scaffold(
          appBar: birdtieAppBar(context, title: Text(title)),
          body: SafeArea(top: false, child: child),
        );

  Widget accessNotice() => _sourceRetired
      ? AccountAccessNotice(
          title: '内容已更新',
          message: '请返回联系人后重新打开。',
          actionLabel: '返回联系人',
          onAction: widget.embedded || !Navigator.of(context).canPop()
              ? null
              : () {
                  if (ModalRoute.of(context)?.isCurrent == true) {
                    Navigator.of(context).maybePop();
                  }
                },
        )
      : widget.auth.signedIn && identity.$3 != null
      ? const AccountAccessNotice(title: '切换身份', message: '请切换到个人身份后添加联系人。')
      : AccountAccessNotice(
          title: '登录后继续',
          message: '登录，与朋友保持联系',
          restoring: widget.auth.sessionRestoring,
          actionLabel: '登录',
          onAction: widget.onSignIn,
        );

  Future<void> openPerson(String accountID) async {
    if (_personOpening || !personal || !validContactAccountID(accountID)) {
      return;
    }
    _personOpening = true;
    try {
      if (widget.onOpenPerson != null) {
        await widget.onOpenPerson!(accountID);
        return;
      }
      final owner = identity, version = epoch, auth = widget.auth;
      final personChanges = changes;
      String? authorization() =>
          current(version, owner) ? auth.authorizationHeader : null;
      // Route dependency/keyboard rebuilds keep this same source binding.
      // Actual identity/transport rebinding still retires the captured epoch.
      final person = PublicPersonPage(
        accountID: accountID,
        auth: auth,
        authorizationHeader: authorization,
        identityChanges: personChanges,
        workspaceID: widget.workspaceID,
        client: client,
        apiBaseUrl: widget.apiBaseUrl,
      );
      await Navigator.of(
        context,
      ).push<void>(MaterialPageRoute(builder: (_) => person));
    } finally {
      _personOpening = false;
    }
  }
}

extension _ContactCodeRequests on _ContactState {
  Future<Map<String, dynamic>> codeRequest(
    String method,
    String path,
    int version,
    (String?, String?, String?) owner, {
    Map<String, dynamic>? body,
  }) async {
    if (!current(version, owner))
      throw const _ContactFailure('内容已更新，请重新打开。', retry: false);
    final base = widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
    if (base.isEmpty) throw const _ContactFailure('尚未连接服务，请稍后重试。');
    final req = http.Request(
      method,
      Uri.parse('${base.replaceFirst(RegExp(r'/$'), '')}/v1/contact-code$path'),
    );
    req.headers['Authorization'] = owner.$1!;
    if (body != null) {
      req.headers['Content-Type'] = 'application/json';
      req.body = jsonEncode(body);
    }
    final response = await http.Response.fromStream(
      await client.send(req).timeout(const Duration(seconds: 12)),
    ).timeout(const Duration(seconds: 12));
    if (!current(version, owner))
      throw const _ContactFailure('内容已更新，请重新打开。', retry: false);
    if (response.statusCode == 401) {
      await widget.onUnauthorized?.call(owner.$1!);
      throw const _ContactFailure('登录已过期，请重新登录。', retry: false);
    }
    if (response.statusCode == 404)
      throw const _ContactFailure('二维码已到期、已撤销或此用户暂不可查看，请重新扫描。', retry: false);
    if (response.statusCode == 403)
      throw const _ContactFailure('请切换到个人身份后继续。', retry: false);
    if (response.statusCode == 409)
      throw const _ContactFailure('二维码已更新，请读取最新状态后继续。');
    if (response.statusCode != 200)
      throw const _ContactFailure('二维码服务暂不可用，请稍后重试。');
    final raw = jsonDecode(utf8.decode(response.bodyBytes));
    if (raw is! Map<String, dynamic> || raw['data'] is! Map<String, dynamic>)
      throw const FormatException();
    return raw['data'] as Map<String, dynamic>;
  }
}

/// The direct user search. It reads the server's current, permission-filtered
/// directory and does not create an Agent task or a connection.
class ContactSearchPage extends _ContactPage {
  const ContactSearchPage({
    super.key,
    required super.auth,
    super.identityChanges,
    super.workspaceID,
    super.client,
    super.apiBaseUrl,
    super.onSignIn,
    super.onUnauthorized,
    super.onOpenPerson,
    super.embedded,
  });
  @override
  State<ContactSearchPage> createState() => _ContactSearchPageState();
}

class _ContactSearchPageState extends _ContactState<ContactSearchPage> {
  final _query = TextEditingController();
  List<(String, String)> _results = [];
  String? _error;
  int _request = 0;
  bool _loading = false, _searched = false;
  bool _retryAllowed = true;

  @override
  void retireIdentity(bool accountChanged) {
    ++_request;
    _results = [];
    _error = null;
    _loading = false;
    _searched = false;
    if (accountChanged) _query.clear();
  }

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  Future<void> _search() async {
    if (!personal) return;
    final query = _query.text.trim();
    if (query.isEmpty) return;
    if (query.runes.length > 80) {
      setState(() => _error = '请输入 80 字以内的姓名或用户标识。');
      return;
    }
    final owner = identity, version = epoch, request = ++_request;
    bool valid() => current(version, owner) && request == _request;
    setState(() {
      _loading = true;
      _error = null;
      _results = [];
      _searched = false;
      _retryAllowed = true;
    });
    try {
      final base = widget.apiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
      if (base.isEmpty) throw const _ContactFailure('尚未连接服务，请稍后重试。');
      final response = await client
          .get(
            Uri.parse(
              '${base.replaceFirst(RegExp(r'/$'), '')}/v1/people/search',
            ).replace(queryParameters: {'q': query}),
            headers: {'Authorization': owner.$1!},
          )
          .timeout(const Duration(seconds: 12));
      if (!valid()) return;
      if (response.statusCode == 401) {
        setState(() {
          _error = '登录已过期，请重新登录。';
          _retryAllowed = false;
        });
        await widget.onUnauthorized?.call(owner.$1!);
        return;
      }
      if (response.statusCode == 403) {
        throw const _ContactFailure('当前身份没有查看权限。', retry: false);
      }
      if (response.statusCode == 400) {
        throw const _ContactFailure('请检查姓名或用户标识。', retry: false);
      }
      if (response.statusCode == 429) {
        throw const _ContactFailure('搜索太频繁，请稍后重试。');
      }
      if (response.statusCode != 200) {
        throw const _ContactFailure('搜索服务暂不可用，请稍后重试。');
      }
      final data =
          (jsonDecode(utf8.decode(response.bodyBytes))
              as Map<String, dynamic>)['data'];
      if (data is! List || data.length > 20) throw const FormatException();
      final seen = <String>{};
      final results = <(String, String)>[];
      for (final item in data) {
        if (item is! Map<String, dynamic> ||
            item['accountId'] is! String ||
            item['displayName'] is! String) {
          throw const FormatException();
        }
        final id = item['accountId'] as String,
            name = item['displayName'] as String;
        if (!validContactAccountID(id) ||
            name.trim().isEmpty ||
            name.runes.length > 160 ||
            !seen.add(id.toLowerCase())) {
          throw const FormatException();
        }
        results.add((id, name));
      }
      if (valid()) {
        setState(() {
          _results = results;
          _searched = true;
        });
      }
    } on TimeoutException {
      if (valid()) setState(() => _error = '搜索超时，请检查网络后重试。');
    } on http.ClientException {
      if (valid()) setState(() => _error = '无法连接服务，请检查网络后重试。');
    } on _ContactFailure catch (error) {
      if (valid()) {
        setState(() {
          _error = error.message;
          _retryAllowed = error.retry;
        });
      }
    } catch (_) {
      if (valid()) setState(() => _error = '搜索结果暂不可读取，请重试。');
    } finally {
      if (valid()) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.of(context);
    return page(
      '搜索用户',
      !personal
          ? accessNotice()
          : ListView(
              padding: const EdgeInsets.all(NowUiTokens.space16),
              children: [
                TextField(
                  key: const Key('contact-user-search'),
                  controller: _query,
                  style: tokens.composer,
                  textInputAction: TextInputAction.search,
                  onSubmitted: (_) => _search(),
                  onChanged: (_) {
                    ++_request;
                    setState(() {
                      _results = [];
                      _searched = false;
                      _loading = false;
                      _error = null;
                    });
                  },
                  decoration: InputDecoration(
                    labelText: '姓名或用户标识',
                    suffixIcon: IconButton(
                      tooltip: '搜索用户',
                      onPressed: _loading ? null : _search,
                      icon: const Icon(Icons.search),
                      constraints: const BoxConstraints(
                        minWidth: 48,
                        minHeight: 48,
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: NowUiTokens.space16),
                if (_loading)
                  Semantics(
                    label: '正在搜索用户',
                    child: const LinearProgressIndicator(),
                  ),
                if (_error != null) ...[
                  Text(_error!, style: tokens.body),
                  if (_retryAllowed)
                    Align(
                      alignment: Alignment.centerLeft,
                      child: TextButton(
                        onPressed: _search,
                        child: const Text('重试'),
                      ),
                    ),
                  if (!_retryAllowed &&
                      _error == '登录已过期，请重新登录。' &&
                      widget.onSignIn != null)
                    Align(
                      alignment: Alignment.centerLeft,
                      child: TextButton(
                        onPressed: widget.onSignIn,
                        child: const Text('登录'),
                      ),
                    ),
                ],
                if (_searched && _results.isEmpty) ...[
                  Text('没有找到可查看的用户。', style: tokens.body),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton(
                      onPressed: () {
                        _query.clear();
                        setState(() => _searched = false);
                      },
                      child: const Text('换个姓名搜索'),
                    ),
                  ),
                ],
                for (final person in _results)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: const CircleAvatar(
                      child: Icon(Icons.person_outline),
                    ),
                    title: Text(
                      person.$2,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: tokens.chrome,
                    ),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: () => openPerson(person.$1),
                  ),
              ],
            ),
    );
  }
}

class _ContactFailure implements Exception {
  const _ContactFailure(this.message, {this.retry = true});
  final String message;
  final bool retry;
}

/// Open this page only from the explicit Scan action. The camera starts on
/// this route and is stopped before any recognized profile is opened.
class ContactScanPage extends _ContactPage {
  const ContactScanPage({
    super.key,
    required super.auth,
    super.identityChanges,
    super.workspaceID,
    super.client,
    super.apiBaseUrl,
    super.onSignIn,
    super.onUnauthorized,
    super.onOpenPerson,
    super.embedded,
    this.scannerController,
  });
  // Supports a platform-backed caller-owned controller and focused lifecycle tests.
  final MobileScannerController? scannerController;
  @override
  State<ContactScanPage> createState() => _ContactScanPageState();
}

class _ContactScanPageState extends _ContactState<ContactScanPage>
    with WidgetsBindingObserver {
  late final MobileScannerController _scanner =
      widget.scannerController ??
      MobileScannerController(
        autoStart: false,
        formats: const [BarcodeFormat.qrCode],
      );
  String? _error;
  bool _opening = false, _starting = false, _paused = false;
  bool _foreground = true;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    WidgetsBinding.instance.addPostFrameCallback((_) => _start());
  }

  @override
  void retireIdentity(bool accountChanged) {
    _error = null;
    _paused = true;
    unawaited(_stop());
    // Reauthentication returns to the same view. Resuming the camera remains
    // an explicit action, and an account change never revives a scanned target.
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    if (!_foreground) {
      unawaited(_stop());
    } else if (!_opening && !_paused && _error == null) {
      unawaited(_start());
    }
  }

  Future<void> _stop() async {
    try {
      await _scanner.stop();
    } catch (_) {
      /* Camera is already stopped. */
    }
  }

  Future<void> _start() async {
    if (!mounted || !personal || !_foreground || _opening || _starting) return;
    final owner = identity, version = epoch;
    _starting = true;
    setState(() {
      _error = null;
      _paused = false;
    });
    try {
      await _scanner.start();
      if (!current(version, owner) || !_foreground || _opening) {
        await _stop();
      } else if (_scanner.value.error case final error?) {
        setState(() => _error = _cameraMessage(error));
      }
    } on MobileScannerException catch (error) {
      if (current(version, owner)) {
        setState(() => _error = _cameraMessage(error));
      }
    } catch (_) {
      if (current(version, owner)) setState(() => _error = '相机暂不可用，请重试或搜索用户。');
    } finally {
      if (mounted) setState(() => _starting = false);
    }
  }

  String _cameraMessage(MobileScannerException error) =>
      switch (error.errorCode) {
        MobileScannerErrorCode.permissionDenied => '相机权限未开启，可在系统设置中开启，或搜索用户。',
        MobileScannerErrorCode.unsupported => '当前设备无法扫码，请搜索用户。',
        _ => '相机暂不可用，请重试或搜索用户。',
      };

  Future<void> _recognized(BarcodeCapture capture) async {
    if (!personal || !_foreground || _opening || _error != null) return;
    final values = capture.barcodes
        .where((code) => code.rawValue != null)
        .toList();
    if (values.isEmpty) return;
    final owner = identity, version = epoch;
    setState(() {
      _opening = true;
      _paused = true;
    });
    await _stop();
    try {
      if (!current(version, owner)) return;
      final code = contactAccountFromCode(values.first.rawValue!);
      final data = await codeRequest(
        'POST',
        '/resolve',
        version,
        owner,
        body: {'codeId': code},
      );
      if (!current(version, owner) || !_foreground) return;
      final accountID = data['accountId'];
      if (data.length != 1 ||
          accountID is! String ||
          !validContactAccountID(accountID))
        throw const FormatException('扫码结果暂不可读取，请重新扫描。');
      await openPerson(accountID);
    } on FormatException catch (error) {
      if (current(version, owner)) setState(() => _error = error.message);
    } on _ContactFailure catch (error) {
      if (current(version, owner)) setState(() => _error = error.message);
    } catch (_) {
      if (current(version, owner)) {
        setState(() => _error = '此用户暂不可查看，请重新扫描或搜索。');
      }
    } finally {
      if (mounted) setState(() => _opening = false);
      // Returning from a profile leaves scanning paused. The same visible QR
      // must not immediately reopen the profile and trap the Back action.
    }
  }

  Future<void> _searchInstead() async {
    if (_opening) return;
    setState(() {
      _opening = true;
      _paused = true;
    });
    await _stop();
    if (!mounted || !personal) {
      _opening = false;
      return;
    }
    try {
      await Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => ContactSearchPage(
            auth: widget.auth,
            identityChanges: widget.identityChanges,
            workspaceID: widget.workspaceID,
            client: client,
            apiBaseUrl: widget.apiBaseUrl,
            onSignIn: widget.onSignIn,
            onUnauthorized: widget.onUnauthorized,
            onOpenPerson: widget.onOpenPerson,
          ),
        ),
      );
    } finally {
      if (mounted) setState(() => _opening = false);
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    unawaited(_releaseScanner());
    super.dispose();
  }

  Future<void> _releaseScanner() async {
    await _stop();
    if (widget.scannerController == null) await _scanner.dispose();
  }

  @override
  Widget build(BuildContext context) => page(
    '扫一扫',
    !personal
        ? accessNotice()
        : ListView(
            padding: const EdgeInsets.all(NowUiTokens.space16),
            children: [
              SizedBox(
                height: (MediaQuery.sizeOf(context).shortestSide * .65).clamp(
                  160.0,
                  320.0,
                ),
                child: ClipRRect(
                  borderRadius: BorderRadius.circular(NowUiTokens.space16),
                  child: MobileScanner(
                    controller: _scanner,
                    onDetect: _recognized,
                    useAppLifecycleState: false,
                    errorBuilder: (_, error) => ColoredBox(
                      color: Theme.of(context).colorScheme.surface,
                      child: const Center(
                        child: Icon(Icons.camera_alt_outlined),
                      ),
                    ),
                    placeholderBuilder: (_) => Center(
                      child: _starting
                          ? const CircularProgressIndicator()
                          : Text(
                              '点击“重新扫描”开启相机。',
                              style: NowUiTokens.of(context).body,
                            ),
                    ),
                  ),
                ),
              ),
              const SizedBox(height: NowUiTokens.space16),
              Text(
                _error ?? (_paused ? '扫描已暂停，可重新扫描。' : '将 Birdtie 联系人二维码放入画面。'),
                style: NowUiTokens.of(context).body,
              ),
              const SizedBox(height: NowUiTokens.space8),
              Wrap(
                spacing: NowUiTokens.space8,
                children: [
                  TextButton(
                    onPressed: _starting || _opening ? null : _start,
                    child: const Text('重新扫描'),
                  ),
                  TextButton(
                    onPressed: _opening ? null : _searchInstead,
                    child: const Text('搜索用户'),
                  ),
                ],
              ),
            ],
          ),
  );
}

/// The opaque QR expires in 24h and can be rotated or revoked by its owner.
class MyContactCodePage extends _ContactPage {
  const MyContactCodePage({
    super.key,
    required super.auth,
    super.identityChanges,
    super.workspaceID,
    super.client,
    super.apiBaseUrl,
    super.onSignIn,
    super.onUnauthorized,
    super.onOpenPerson,
    super.embedded,
  });
  @override
  State<MyContactCodePage> createState() => _MyContactCodePageState();
}

class _MyContactCodePageState extends _ContactState<MyContactCodePage> {
  ContactCodeView? _code;
  String? _error;
  bool _busy = false, _unknownWrite = false;
  Timer? _expiry;
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void retireIdentity(bool accountChanged) {
    _expiry?.cancel();
    _code = null;
    _busy = false;
    _error = null;
    _unknownWrite = false;
    if (personal) WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void dispose() {
    _expiry?.cancel();
    super.dispose();
  }

  void _accept(ContactCodeView code) {
    _code = code;
    _expiry?.cancel();
    final remaining = code.expiresAt?.difference(DateTime.now());
    if (remaining != null && !remaining.isNegative)
      _expiry = Timer(remaining, () {
        if (mounted) setState(() {});
      });
  }

  Future<void> _load() async => _perform(false);
  Future<void> _perform(bool change, {bool revoke = false}) async {
    if (_busy || !personal || change && (_code == null || _unknownWrite))
      return;
    final owner = identity, version = epoch;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      Map<String, dynamic>? body;
      if (change) {
        final random = Random.secure();
        final b = List.generate(16, (_) => random.nextInt(256));
        b[6] = (b[6] & 15) | 64;
        b[8] = (b[8] & 63) | 128;
        final hex = b.map((v) => v.toRadixString(16).padLeft(2, '0')).join();
        final op =
            '${hex.substring(0, 8)}-${hex.substring(8, 12)}-${hex.substring(12, 16)}-${hex.substring(16, 20)}-${hex.substring(20)}';
        body = {'operationId': op, 'expectedVersion': _code!.version};
      }
      final data = await codeRequest(
        change
            ? revoke
                  ? 'DELETE'
                  : 'POST'
            : 'GET',
        '',
        version,
        owner,
        body: body,
      );
      final code = ContactCodeView.fromJson(data);
      if (current(version, owner))
        setState(() {
          _accept(code);
          _unknownWrite = false;
        });
    } on _ContactFailure catch (e) {
      if (current(version, owner))
        setState(() {
          _error = e.message;
          _unknownWrite = _unknownWrite || change;
        });
    } catch (_) {
      if (current(version, owner))
        setState(() {
          _error = change ? '操作结果待核实，请读取最新状态。' : '二维码暂不可读取，请检查网络后重试。';
          _unknownWrite = _unknownWrite || change;
        });
    } finally {
      if (current(version, owner)) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.of(context), id = widget.auth.accountID;
    final active = _code?.activeAt(DateTime.now()) == true;
    return page(
      '我的二维码',
      !personal
          ? accessNotice()
          : id == null || !validContactAccountID(id)
          ? const AccountAccessNotice(title: '二维码暂不可用', message: '请重新登录后查看。')
          : ListView(
              padding: const EdgeInsets.all(NowUiTokens.space24),
              children: [
                Text(
                  widget.auth.displayName ?? 'Birdtie 用户',
                  style: tokens.pageTitle,
                ),
                const SizedBox(height: NowUiTokens.space16),
                if (_busy) const LinearProgressIndicator(),
                if (_error != null) Text(_error!, style: tokens.body),
                if (active && !_unknownWrite)
                  Center(
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(maxWidth: 280),
                      child: Semantics(
                        label: '我的 Birdtie 联系人二维码',
                        image: true,
                        child: QrImageView(
                          key: ValueKey('contact-code:${_code!.codeID}'),
                          data: contactCodeFor(_code!.codeID!),
                          backgroundColor: Colors.white,
                          padding: const EdgeInsets.all(NowUiTokens.space16),
                        ),
                      ),
                    ),
                  ),
                const SizedBox(height: NowUiTokens.space16),
                Text(
                  _code == null
                      ? '正在读取二维码状态。'
                      : active
                      ? '此码 24 小时内有效，扫码后仍需检查资料权限。'
                      : _code!.codeID == null
                      ? '尚无有效二维码，可生成一个。'
                      : '二维码已到期，请更新后分享。',
                  style: tokens.body,
                ),
                if (active)
                  Text(
                    '到期时间：${_code!.expiresAt!.toLocal().toString().substring(0, 16)}',
                    style: tokens.metadata,
                  ),
                Wrap(
                  spacing: NowUiTokens.space8,
                  children: [
                    TextButton(
                      onPressed: _busy ? null : _load,
                      child: Text(_unknownWrite ? '核实最新状态' : '刷新状态'),
                    ),
                    if (_code != null && !_unknownWrite)
                      TextButton(
                        onPressed: _busy ? null : () => _perform(true),
                        child: Text(active ? '更新二维码' : '生成二维码'),
                      ),
                    if (_code?.codeID != null && !_unknownWrite)
                      TextButton(
                        onPressed: _busy
                            ? null
                            : () => _perform(true, revoke: true),
                        child: const Text('撤销二维码'),
                      ),
                  ],
                ),
              ],
            ),
    );
  }
}
