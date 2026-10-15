import 'package:flutter/material.dart';

import 'birdtie_auth_controller.dart';

class DevPhoneLoginSheet extends StatefulWidget {
  const DevPhoneLoginSheet({
    super.key,
    required this.auth,
    this.isCurrent,
    this.entryChanges,
  });
  final BirdtieAuthController auth;
  final bool Function()? isCurrent;
  final Listenable? entryChanges;

  @override
  State<DevPhoneLoginSheet> createState() => _DevPhoneLoginSheetState();
}

class _DevPhoneLoginSheetState extends State<DevPhoneLoginSheet> {
  final _phone = TextEditingController();
  final _code = TextEditingController(text: '123456');
  bool _requested = false;
  bool _email = false;
  int _entry = 0;
  bool _finished = false;
  bool get _current => widget.isCurrent?.call() ?? true;

  @override
  void dispose() {
    _phone.dispose();
    _code.dispose();
    super.dispose();
  }

  Future<void> _request() async {
    if (!_current || _finished) return;
    final entry = _entry;
    final email = _email;
    bool current() => mounted && _current && entry == _entry && email == _email;
    final accepted = email
        ? await widget.auth.requestDevEmailCode(_phone.text, isCurrent: current)
        : await widget.auth.requestDevPhoneCode(
            _phone.text,
            isCurrent: current,
          );
    if (current() && accepted) setState(() => _requested = true);
  }

  Future<void> _verify() async {
    if (!_current || _finished) return;
    final entry = _entry;
    final email = _email;
    bool current() => mounted && _current && entry == _entry && email == _email;
    final signedIn = email
        ? await widget.auth.verifyDevEmailCode(
            _phone.text,
            _code.text,
            isCurrent: current,
          )
        : await widget.auth.verifyDevPhoneCode(
            _phone.text,
            _code.text,
            isCurrent: current,
          );
    if (!mounted) return;
    if (current() && signedIn && ModalRoute.of(context)?.isCurrent == true) {
      _finished = true;
      Navigator.pop(context);
    }
  }

  @override
  Widget build(BuildContext context) => SafeArea(
    top: false,
    child: Padding(
      padding: EdgeInsets.fromLTRB(
        24,
        20,
        24,
        MediaQuery.viewInsetsOf(context).bottom + 24,
      ),
      child: SingleChildScrollView(
        child: AnimatedBuilder(
          animation: Listenable.merge([widget.auth, widget.entryChanges]),
          builder: (context, _) => Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (!_current) ...[
                const Text('账户或入口已变化，请返回后重新打开。'),
                TextButton(
                  onPressed: () {
                    if (!_finished &&
                        ModalRoute.of(context)?.isCurrent == true) {
                      _finished = true;
                      Navigator.pop(context);
                    }
                  },
                  child: const Text('返回'),
                ),
              ] else ...[
                Text('开发测试登录', style: Theme.of(context).textTheme.titleLarge),
                const SizedBox(height: 8),
                Text(
                  _email
                      ? '使用原登录邮箱接续旧账号；固定验证码不发送邮件，也不验证邮箱所有权。'
                      : '固定验证码仅用于开发账号，不发送短信，也不验证手机号所有权。',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                const SizedBox(height: 16),
                if (widget.auth.devEmailAvailable) ...[
                  SegmentedButton<bool>(
                    segments: const [
                      ButtonSegment(value: false, label: Text('手机号')),
                      ButtonSegment(value: true, label: Text('邮箱')),
                    ],
                    selected: {_email},
                    onSelectionChanged: widget.auth.busy
                        ? null
                        : (values) {
                            setState(() {
                              _email = values.single;
                              _entry++;
                              _requested = false;
                              _phone.clear();
                              _code.text = '123456';
                            });
                          },
                  ),
                  const SizedBox(height: 16),
                ],
                TextField(
                  controller: _phone,
                  enabled: !_requested && !widget.auth.busy,
                  keyboardType: _email
                      ? TextInputType.emailAddress
                      : TextInputType.phone,
                  autocorrect: false,
                  enableSuggestions: false,
                  textInputAction: TextInputAction.done,
                  decoration: InputDecoration(
                    labelText: _email ? '原登录邮箱' : '手机号',
                    hintText: _email ? '例如 name@example.com' : '例如 13800138000',
                    border: OutlineInputBorder(),
                  ),
                ),
                const SizedBox(height: 14),
                if (_requested) ...[
                  TextField(
                    controller: _code,
                    keyboardType: TextInputType.number,
                    maxLength: 6,
                    decoration: const InputDecoration(
                      labelText: '测试验证码',
                      border: OutlineInputBorder(),
                    ),
                  ),
                  Text(
                    '默认测试码为 123456；5 分钟内有效。',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  const SizedBox(height: 12),
                ],
                if (widget.auth.error != null) ...[
                  Text(
                    widget.auth.error!,
                    style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                  const SizedBox(height: 8),
                ],
                FilledButton(
                  onPressed: widget.auth.busy
                      ? null
                      : _requested
                      ? _verify
                      : _request,
                  child: Text(
                    widget.auth.busy
                        ? '请稍候…'
                        : _requested
                        ? '登录 Birdtie'
                        : '获取测试验证码',
                  ),
                ),
                if (_requested)
                  TextButton(
                    onPressed: widget.auth.busy
                        ? null
                        : () => setState(() {
                            _requested = false;
                            _entry++;
                            _code.text = '123456';
                          }),
                    child: Text(_email ? '更换邮箱' : '更换手机号'),
                  ),
              ],
            ],
          ),
        ),
      ),
    ),
  );
}
