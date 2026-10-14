import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../workspace/now_ui_tokens.dart';

/// Application navigation titles use the same system scaling as page content.
/// Current callers use plain Text titles and icon/popup actions. Keep AppBar's
/// navigation and semantics, while reserving measured height for wrapping text.
AppBar birdtieAppBar(
  BuildContext context, {
  required Text title,
  Widget? leading,
  List<Widget>? actions,
  double? toolbarHeight,
}) {
  assert(title.data != null, 'Navigation titles must use plain Text.');
  final media = MediaQuery.of(context);
  final theme = Theme.of(context);
  final style = NowUiTokens.of(context).pageTitle;
  final direction = title.textDirection ?? Directionality.of(context);
  final locale = title.locale ?? Localizations.maybeLocaleOf(context);
  final scaffold = Scaffold.maybeOf(context);
  final route = ModalRoute.of(context);
  final hasLeading =
      leading != null ||
      (scaffold?.hasDrawer ?? false) ||
      (route?.impliesAppBarDismissal ?? false);
  final leadingWidth = hasLeading ? (theme.appBarTheme.leadingWidth ?? 56) : 0;
  final spacing =
      theme.appBarTheme.titleSpacing ?? NavigationToolbar.kMiddleSpacing;
  // All existing actions are IconButton or icon-only PopupMenuButton. Explicit
  // 48px slots keep their hit regions and our title width measurement aligned.
  final actionWidgets = [
    for (final action in actions ?? const <Widget>[])
      SizedBox.square(dimension: 48, child: action),
  ];
  final actionPadding =
      theme.appBarTheme.actionsPadding?.resolve(direction).horizontal ?? 0;
  final trailingWidth = actionWidgets.length * 48 + actionPadding;
  final titleWidth = math.max(
    1.0,
    media.size.width -
        media.padding.horizontal -
        leadingWidth -
        trailingWidth -
        spacing * 2,
  );
  final painter = TextPainter(
    text: TextSpan(text: title.data, style: style),
    textScaler: media.textScaler,
    textDirection: direction,
    locale: locale,
    strutStyle: title.strutStyle,
    textHeightBehavior: title.textHeightBehavior,
  )..layout(maxWidth: titleWidth);
  final measuredHeight = painter.height.ceilToDouble() + 16;
  painter.dispose();
  final minimumHeight =
      toolbarHeight ?? theme.appBarTheme.toolbarHeight ?? kToolbarHeight;
  final visibleHeight = math.max(
    0.0,
    media.size.height - media.padding.vertical - media.viewInsets.bottom,
  );
  // Very long names must leave room for page content and the keyboard. This
  // limits the scroll viewport, never the font scale or number of title lines.
  final maximumHeight = math.max(minimumHeight, visibleHeight * .4);
  final resolvedHeight = math.min(
    math.max(minimumHeight, measuredHeight),
    maximumHeight,
  );
  final titleText = ExcludeSemantics(
    child: Text(
      title.data!,
      key: title.key,
      style: style,
      strutStyle: title.strutStyle,
      textAlign: title.textAlign,
      textDirection: direction,
      locale: locale,
      textScaler: media.textScaler,
      softWrap: true,
      overflow: TextOverflow.visible,
      textWidthBasis: title.textWidthBasis,
      textHeightBehavior: title.textHeightBehavior,
      selectionColor: title.selectionColor,
    ),
  );
  final titleContent = measuredHeight > resolvedHeight
      ? SingleChildScrollView(
          key: const Key('birdtie-app-bar-title-scroll'),
          primary: false,
          child: titleText,
        )
      : titleText;
  return AppBar(
    leading: leading,
    actions: actions == null ? null : actionWidgets,
    toolbarHeight: resolvedHeight,
    titleTextStyle: style,
    title: MediaQuery(
      // AppBar clamps its default title MediaQuery to 1.34. Restore the outer
      // system TextScaler inside the title without changing any other content.
      data: media,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: ConstrainedBox(
          constraints: BoxConstraints(maxHeight: resolvedHeight - 16),
          child: Semantics(
            label: title.semanticsLabel ?? title.data,
            child: titleContent,
          ),
        ),
      ),
    ),
  );
}
