import 'package:flutter/material.dart';

import 'birdtie_auth_controller.dart';
import 'dev_phone_login_sheet.dart';

final _openLoginSheets = Expando<bool>();

// Choosing a method only starts the existing authentication flow. Canceling
// never repeats an Agent query, sends a message or mutates a domain resource.
Future<void> showBirdtieLoginSheet(
  BuildContext context,
  BirdtieAuthController auth, {
  bool Function()? isCurrent,
  Listenable? entryChanges,
}) async {
  bool current() => context.mounted && (isCurrent == null || isCurrent());
  if (!current() ||
      auth.signedIn ||
      auth.sessionRestoring ||
      auth.busy ||
      _openLoginSheets[auth] == true)
    return;
  _openLoginSheets[auth] = true;
  try {
    final method = await showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => BirdtieLoginSheet(
        auth: auth,
        isCurrent: current,
        entryChanges: entryChanges,
      ),
    );
    if (!current() || auth.signedIn || auth.sessionRestoring || auth.busy) {
      return;
    }
    if (method == 'dev_phone' && auth.devPhoneAvailable) {
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        showDragHandle: true,
        builder: (_) => DevPhoneLoginSheet(
          auth: auth,
          isCurrent: current,
          entryChanges: entryChanges,
        ),
      );
    } else if (method == 'oidc' && auth.available) {
      await auth.signIn(isCurrent: current);
    }
  } finally {
    _openLoginSheets[auth] = false;
  }
}

class BirdtieLoginSheet extends StatefulWidget {
  const BirdtieLoginSheet({
    super.key,
    required this.auth,
    this.isCurrent,
    this.entryChanges,
  });
  final BirdtieAuthController auth;
  final bool Function()? isCurrent;
  final Listenable? entryChanges;

  @override
  State<BirdtieLoginSheet> createState() => _BirdtieLoginSheetState();
}

class _BirdtieLoginSheetState extends State<BirdtieLoginSheet> {
  bool _choiceConsumed = false;
  BirdtieAuthController get auth => widget.auth;
  bool get current => widget.isCurrent?.call() ?? true;
  void _close([String? method]) {
    if (_choiceConsumed || ModalRoute.of(context)?.isCurrent != true) return;
    _choiceConsumed = true;
    Navigator.pop(context, current ? method : null);
  }

  @override
  Widget build(BuildContext context) => SafeArea(
    top: false,
    child: SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(
        24,
        12,
        24,
        MediaQuery.viewInsetsOf(context).bottom + 24,
      ),
      child: AnimatedBuilder(
        animation: Listenable.merge([auth, widget.entryChanges]),
        builder: (context, _) => Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('登录 Birdtie', style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 12),
            if (!current)
              const Text('账户或入口已变化，请返回后重新打开。')
            else if (auth.sessionRestoring)
              const Text('正在恢复账户，请稍候。')
            else if (auth.signedIn)
              const Text('账户已连接。')
            else ...[
              const Text('登录后查看本人的消息和活动。你仍可以浏览公开城市内容。'),
              const SizedBox(height: 16),
              if (auth.available)
                FilledButton(
                  onPressed: auth.busy ? null : () => _close('oidc'),
                  style: FilledButton.styleFrom(
                    minimumSize: const Size.fromHeight(48),
                  ),
                  child: const Text('继续登录'),
                ),
              if (auth.devPhoneAvailable) ...[
                const SizedBox(height: 8),
                OutlinedButton(
                  onPressed: auth.busy ? null : () => _close('dev_phone'),
                  style: OutlinedButton.styleFrom(
                    minimumSize: const Size.fromHeight(48),
                  ),
                  child: const Text('手机号测试登录'),
                ),
              ],
              if (!auth.available && !auth.devPhoneAvailable) ...[
                Text(
                  auth.configurationChecked ? '登录服务暂不可用，请稍后再试。' : '正在检查登录服务…',
                ),
                if (auth.configurationChecked || auth.configurationChecking)
                  TextButton(
                    onPressed: auth.configurationChecking || auth.busy
                        ? null
                        : () {
                            if (current &&
                                !auth.signedIn &&
                                !auth.sessionRestoring) {
                              auth.refreshLoginConfiguration();
                            }
                          },
                    child: Text(auth.configurationChecking ? '正在检查…' : '重新检查'),
                  ),
              ],
            ],
            const SizedBox(height: 8),
            TextButton(onPressed: _close, child: const Text('返回')),
          ],
        ),
      ),
    ),
  );
}
