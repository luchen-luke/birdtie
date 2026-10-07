import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

class TopControls extends StatelessWidget {
  const TopControls({
    super.key,
    required this.onSidebar,
    required this.onInbox,
    required this.onTools,
    required this.contextLabel,
    this.onContext,
    this.onHeightChanged,
  });
  final VoidCallback onSidebar;
  final VoidCallback onInbox;
  final VoidCallback onTools;
  final String contextLabel;
  final VoidCallback? onContext;
  final ValueChanged<double>? onHeightChanged;

  @override
  Widget build(BuildContext context) => _TopControlsMeasure(
    onHeight: (height) => onHeightChanged?.call(height),
    child: SafeArea(
      bottom: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
        child: Row(
          children: [
            _button(Icons.menu_rounded, '打开侧边栏', onSidebar),
            Expanded(
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 8),
                child: Material(
                  color: const Color(0xFFFCFBF8),
                  borderRadius: BorderRadius.circular(24),
                  child: InkWell(
                    key: const Key('now-city-picker'),
                    onTap: onContext,
                    borderRadius: BorderRadius.circular(24),
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(minHeight: 48),
                      child: Center(
                        child: Padding(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 10,
                            vertical: 8,
                          ),
                          child: Text(
                            '当前：$contextLabel',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            textAlign: TextAlign.center,
                            style: const TextStyle(
                              color: Color(0xFF193B32),
                              fontSize: 12,
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
            _button(Icons.inbox_outlined, '打开收件箱', onInbox),
          ],
        ),
      ),
    ),
  );

  Widget _button(IconData icon, String tooltip, VoidCallback onPressed) =>
      Material(
        color: const Color(0xFFFCFBF8),
        elevation: 4,
        shadowColor: Colors.black.withValues(alpha: 0.12),
        shape: const CircleBorder(),
        child: IconButton(
          constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
          icon: Icon(icon, color: const Color(0xFF193B32)),
          tooltip: tooltip,
          onPressed: onPressed,
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
