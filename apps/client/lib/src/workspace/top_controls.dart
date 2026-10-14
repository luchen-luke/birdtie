import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'now_ui_tokens.dart';
import 'now_glass_surface.dart';

class TopControls extends StatelessWidget {
  const TopControls({
    super.key,
    required this.onSidebar,
    required this.onInbox,
    required this.onTools,
    required this.contextLabel,
    this.onContext,
    this.onHeightChanged,
    this.socialExpanded = false,
    this.onNotifications,
    this.notificationsExpanded = false,
    this.hasUnreadMessages = false,
    this.hasUnreadNotifications = false,
  });
  final VoidCallback onSidebar, onInbox, onTools;
  final String contextLabel;
  final VoidCallback? onContext;
  final ValueChanged<double>? onHeightChanged;
  final bool socialExpanded;
  final VoidCallback? onNotifications;
  final bool notificationsExpanded, hasUnreadMessages, hasUnreadNotifications;
  // Keep the existing geometry gap below the controls; no map-wide fade.
  static const mapFadeTail = 20.0;

  @override
  Widget build(BuildContext context) => _TopControlsMeasure(
    onHeight: (height) => onHeightChanged?.call(height - mapFadeTail),
    child: SafeArea(
      bottom: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(8, 0, 8, mapFadeTail),
        child: Row(
          children: [
            _tool(
              context,
              Icons.menu_rounded,
              '打开侧边栏',
              onSidebar,
              'now-sidebar',
            ),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 4),
                child: Align(
                  alignment: Alignment.centerLeft,
                  heightFactor: 1,
                  child: Tooltip(
                    message: '选择城市：$contextLabel',
                    child: NowGlassSurface(
                      radius: 16,
                      backdropBlur: false,
                      child: Material(
                        type: MaterialType.transparency,
                        child: InkWell(
                          key: const Key('now-city-picker'),
                          onTap: onContext,
                          borderRadius: BorderRadius.circular(12),
                          child: ConstrainedBox(
                            constraints: const BoxConstraints(minHeight: 48),
                            child: Center(
                              widthFactor: 1,
                              heightFactor: 1,
                              child: Padding(
                                padding: const EdgeInsets.symmetric(
                                  horizontal: 4,
                                  vertical: 8,
                                ),
                                child: Row(
                                  mainAxisSize: MainAxisSize.min,
                                  children: [
                                    Flexible(
                                      child: Text(
                                        contextLabel,
                                        style: NowUiTokens.reviewedOf(
                                          context,
                                        ).chrome,
                                      ),
                                    ),
                                    const SizedBox(width: 3),
                                    const Icon(
                                      Icons.arrow_drop_down_rounded,
                                      size: 20,
                                    ),
                                  ],
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
            _tool(
              context,
              socialExpanded ? Icons.people : Icons.people_outline,
              socialExpanded ? '收起联系' : '联系',
              onInbox,
              'now-social',
              unread: hasUnreadMessages,
            ),
            if (onNotifications != null) const SizedBox(width: 4),
            if (onNotifications != null)
              _tool(
                context,
                notificationsExpanded
                    ? Icons.notifications
                    : Icons.notifications_outlined,
                notificationsExpanded ? '收起通知' : '通知',
                onNotifications!,
                'now-notifications',
                unread: hasUnreadNotifications,
              ),
          ],
        ),
      ),
    ),
  );

  Widget _tool(
    BuildContext context,
    IconData icon,
    String tooltip,
    VoidCallback action,
    String name, {
    bool unread = false,
  }) => SizedBox(
    key: Key('$name-button'),
    width: 48,
    height: 48,
    child: Semantics(
      container: true,
      label: unread ? '$tooltip，有未读' : tooltip,
      button: true,
      enabled: true,
      onTap: action,
      child: ExcludeSemantics(
        child: NowGlassSurface(
          radius: 16,
          backdropBlur: false,
          child: IconButton(
            tooltip: tooltip,
            onPressed: action,
            padding: EdgeInsets.zero,
            constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
            icon: Badge(
              isLabelVisible: unread,
              smallSize: 6,
              child: Icon(
                icon,
                size: 20,
                color: NowUiTokens.of(context).chrome.color,
              ),
            ),
          ),
        ),
      ),
    ),
  );
}

class _TopControlsMeasure extends SingleChildRenderObjectWidget {
  const _TopControlsMeasure({required this.onHeight, required super.child});
  final ValueChanged<double> onHeight;
  @override
  RenderObject createRenderObject(BuildContext context) =>
      _TopControlsRender(onHeight);
  @override
  void updateRenderObject(
    BuildContext context,
    covariant _TopControlsRender renderObject,
  ) => renderObject.onHeight = onHeight;
}

class _TopControlsRender extends RenderProxyBox {
  _TopControlsRender(this.onHeight);
  ValueChanged<double> onHeight;
  double? _reported;
  @override
  void performLayout() {
    super.performLayout();
    if (_reported == size.height) return;
    _reported = size.height;
    final height = size.height;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (attached) onHeight(height);
    });
  }
}
