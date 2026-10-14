import 'dart:math' as math;

import 'now_ui_tokens.dart';

/// Geometry in the full root viewport. Only this owner consumes the IME.
/// The background reaches the viewport/IME edge; the content consumes the
/// remaining system gesture safe area once, inside that background.
class NowWorkspaceLayout {
  const NowWorkspaceLayout._({
    required this.backgroundBottom,
    required this.contentSafeBottom,
    required this.availableHeight,
    required this.maxContentHeight,
    required this.contentHeight,
    required this.surfaceHeight,
    required this.surfaceInset,
  });

  factory NowWorkspaceLayout.resolve({
    required double rootHeight,
    required double safeTop,
    required double safeBottom,
    required double keyboard,
    required double chromeBottom,
    required double composerHeight,
    required bool hasContent,
    required bool expanded,
    required bool editing,
    double? rootWidth,
    double? resultContentHeight,
    double textScale = 1,
    double minimumComposerHeight = NowUiTokens.composerMinHeight,
  }) {
    final height = math.max(0.0, rootHeight);
    final backgroundBottom = keyboard.clamp(0.0, height).toDouble();
    final contentSafeBottom = (safeBottom - backgroundBottom)
        .clamp(0.0, height - backgroundBottom)
        .toDouble();
    final safeHeight = math.max(
      0.0,
      height - backgroundBottom - safeTop - contentSafeBottom,
    );
    final normalAvailable = math.max(
      0.0,
      height - backgroundBottom - contentSafeBottom - chromeBottom - 12,
    );
    final idealInset = hasContent ? NowUiTokens.space12 : NowUiTokens.space8;
    // Short landscape IMEs surrender decorative gaps before a scaled line.
    // Neither this calculation nor a keyboard change recreates the map/editor.
    final inputPriority =
        editing && normalAvailable < minimumComposerHeight + 2 * idealInset;
    final available = inputPriority ? safeHeight : normalAvailable;
    final composer = math.max(composerHeight, minimumComposerHeight);
    final minimumContent = minimumComposerHeight + 2 * idealInset;
    final full = hasContent && (expanded || editing);
    final portrait = rootWidth == null || height >= rootWidth;
    // 40% is a preferred normal-portrait reading ceiling, not an accessibility
    // or landscape cap. A measured short result still shrinks naturally there.
    final preferShortCeiling = portrait && textScale <= 1.3;
    final shortMax = preferShortCeiling
        ? math.min(
            available,
            math.max(minimumContent, safeHeight * .4 - contentSafeBottom),
          )
        : available;
    final maxContentHeight = hasContent && !full ? shortMax : available;
    final desired = full
        ? available
        : !hasContent
        ? composer + 2 * idealInset
        : resultContentHeight == null
        // While the content has not been measured, allow an internally
        // scrolling content region up to the safe reading ceiling. No guessed
        // chat row height or Spacer fabricates a natural result.
        ? maxContentHeight
        : composer + 2 * idealInset + math.max(0.0, resultContentHeight);
    final contentHeight = math.min(maxContentHeight, desired);
    final surfaceInset = math.min(
      idealInset,
      math.max(0.0, (contentHeight - minimumComposerHeight) / 2),
    );
    return NowWorkspaceLayout._(
      backgroundBottom: backgroundBottom,
      contentSafeBottom: contentSafeBottom,
      availableHeight: available,
      maxContentHeight: maxContentHeight,
      contentHeight: contentHeight,
      surfaceHeight: contentHeight + contentSafeBottom,
      surfaceInset: surfaceInset,
    );
  }

  /// Compatibility alias. This is the background edge, never a SafeArea inset.
  double get bottom => backgroundBottom;

  final double backgroundBottom, contentSafeBottom;

  /// Heights here exclude the internally consumed gesture safe area.
  final double availableHeight, maxContentHeight, contentHeight;

  /// Total background height, including contentSafeBottom exactly once.
  final double surfaceHeight;

  /// Each of the top/bottom interior gaps; shrinks before clipping a text line.
  final double surfaceInset;
}
