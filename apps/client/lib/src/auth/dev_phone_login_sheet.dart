import 'package:flutter/material.dart';

import 'birdtie_auth_controller.dart';

class DevPhoneLoginSheet extends StatefulWidget {
  const DevPhoneLoginSheet({super.key, required this.auth});
  final BirdtieAuthController auth;

  @override
  State<DevPhoneLoginSheet> createState() => _DevPhoneLoginSheetState();
}

class _DevPhoneLoginSheetState extends State<DevPhoneLoginSheet> {
  final _phone = TextEditingController();
  final _code = TextEditingController(text: '123456');
  bool _requested = false;

  @override
  void dispose() {
    _phone.dispose();
    _code.dispose();
    super.dispose();
  }

  Future<void> _request() async {
    final accepted = await widget.auth.requestDevPhoneCode(_phone.text);
    if (mounted && accepted) setState(() => _requested = true);
  }

  Future<void> _verify() async {
    final signedIn = await widget.auth.verifyDevPhoneCode(
      _phone.text,
      _code.text,
    );
    if (mounted && signedIn) Navigator.pop(context);
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
          animation: widget.auth,
          builder: (context, _) => Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text(
                '本地测试登录',
                style: TextStyle(fontSize: 23, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 8),
              const Text(
                '此模式只连接本机 Birdtie API，不发送短信，也不验证手机号所有权。',
                style: TextStyle(color: Color(0xFF747B73)),
              ),
              const SizedBox(height: 22),
              TextField(
                controller: _phone,
                enabled: !_requested && !widget.auth.busy,
                keyboardType: TextInputType.phone,
                textInputAction: TextInputAction.done,
                decoration: const InputDecoration(
                  labelText: '手机号',
                  hintText: '例如 13800138000',
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
                const Text(
                  '默认测试码为 123456；5 分钟内有效。',
                  style: TextStyle(color: Color(0xFF747B73), fontSize: 12),
                ),
                const SizedBox(height: 12),
              ],
              if (widget.auth.error != null) ...[
                Text(
                  widget.auth.error!,
                  style: const TextStyle(color: Color(0xFF9C4939)),
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
                          _code.text = '123456';
                        }),
                  child: const Text('更换手机号'),
                ),
            ],
          ),
        ),
      ),
    ),
  );
}
