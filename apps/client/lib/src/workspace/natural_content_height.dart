import 'package:flutter/rendering.dart';
import 'package:flutter/widgets.dart';

/// Measures the laid-out child, not a viewport or a lazy-list extent estimate.
class NaturalContentHeight extends SingleChildRenderObjectWidget {
  const NaturalContentHeight({
    super.key,
    required this.onHeight,
    required super.child,
  });
  final ValueChanged<double> onHeight;

  @override
  RenderObject createRenderObject(BuildContext context) =>
      _NaturalHeightRender(onHeight);

  @override
  void updateRenderObject(
    BuildContext context,
    covariant RenderObject renderObject,
  ) {
    (renderObject as _NaturalHeightRender).onHeight = onHeight;
  }
}

class _NaturalHeightRender extends RenderProxyBox {
  _NaturalHeightRender(this._onHeight);
  ValueChanged<double> _onHeight;
  int _generation = 0;
  set onHeight(ValueChanged<double> value) {
    _onHeight = value;
    markNeedsLayout();
  }

  @override
  void performLayout() {
    super.performLayout();
    final height = child?.size.height;
    if (height == null || !height.isFinite) return;
    final generation = ++_generation;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (attached && generation == _generation) _onHeight(height);
    });
  }
}
