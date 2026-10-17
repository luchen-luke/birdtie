import 'dart:async';
import 'package:flutter/material.dart';
import '../app/birdtie_app_bar.dart';
import '../workspace/now_ui_tokens.dart';
import 'personal_collection_page.dart';
import 'selected_place_journey.dart';

/// Selection is a draft operation; the existing editor owns actual saving.
class SelectedPlaceJourneyPage extends StatefulWidget {
  const SelectedPlaceJourneyPage({
    super.key,
    required this.controller,
    required this.cityName,
  });
  final SelectedPlaceJourneyController controller;
  final String cityName;
  @override
  State<SelectedPlaceJourneyPage> createState() =>
      _SelectedPlaceJourneyPageState();
}

class _SelectedPlaceJourneyPageState extends State<SelectedPlaceJourneyPage> {
  SelectedPlaceJourneyController get c => widget.controller;
  @override
  void initState() {
    super.initState();
    unawaited(c.load());
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: c,
    builder: (context, _) {
      final tokens = NowUiTokens.of(context);
      if (c.current && c.editing) {
        final receipt = c.receipt;
        return PopScope(
          canPop:
              !c.collections.saving &&
              !c.collections.uncertain &&
              !c.collections.controlBlocked,
          onPopInvokedWithResult: (didPop, _) {
            if (!didPop)
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(content: Text('请先核实当前保存结果，再返回对话。')),
              );
          },
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Material(
                key: const Key('selected-place-journey-editor-header'),
                color: Theme.of(context).colorScheme.surface,
                child: SafeArea(
                  bottom: false,
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
                    child: Semantics(
                      liveRegion: true,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            c.recoveringOriginal
                                ? '请先核实本人原旅程的保存结果'
                                : receipt == null
                                ? '${c.placeName} · 第${c.day}天，检查后保存'
                                : '已核实保存到“${receipt.title}”第${c.day}天',
                            style: tokens.body,
                          ),
                          if (c.verifying) const LinearProgressIndicator(),
                          if (c.recoveringOriginal &&
                              c.collections.canSwitchDraft)
                            TextButton(
                              onPressed: () =>
                                  unawaited(c.finishOriginalRecovery()),
                              child: const Text('继续选择当前地点的旅程'),
                            ),
                          if (c.error != null) ...[
                            Text(c.error!, style: tokens.body),
                            TextButton(
                              onPressed: c.verifying
                                  ? null
                                  : () => unawaited(c.verifySaved()),
                              child: const Text('核实保存结果'),
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                ),
              ),
              Expanded(
                child: MediaQuery.removePadding(
                  context: context,
                  removeTop: true,
                  child: PersonalCollectionPage(
                    controller: c.collections,
                    cityName: widget.cityName,
                  ),
                ),
              ),
            ],
          ),
        );
      }
      return Scaffold(
        appBar: birdtieAppBar(context, title: Text('加入第${c.day}天')),
        body: SafeArea(
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              if (!c.current)
                Semantics(
                  liveRegion: true,
                  child: Text(c.error ?? '所选地点已变化，请返回重新选择。'),
                )
              else ...[
                Text(c.placeName ?? '正在读取当前地点…', style: tokens.placeTitle),
                const SizedBox(height: 8),
                const Text('选择本人旅程草稿，检查后再保存。'),
                if (c.busy) const LinearProgressIndicator(),
                if (c.error != null)
                  Semantics(liveRegion: true, child: Text(c.error!)),
                const SizedBox(height: 16),
                for (final target in c.targets)
                  ListTile(
                    contentPadding: EdgeInsets.zero,
                    title: Text(target.title.isEmpty ? '未命名旅程' : target.title),
                    subtitle: Text(
                      '本人草稿 · ${target.journeyDetails!.itinerary.length}项安排',
                    ),
                    trailing: const Icon(Icons.chevron_right),
                    enabled: !c.busy,
                    onTap: c.busy ? null : () => unawaited(c.choose(target)),
                  ),
                if (!c.busy && c.targets.isEmpty && c.error == null)
                  const Text('这座城市还没有可加入的旅程草稿，可以新建一份。'),
                if (c.collections.nextCursor != null)
                  TextButton(
                    onPressed: c.busy
                        ? null
                        : () => unawaited(c.load(more: true)),
                    child: const Text('继续读取旅程'),
                  ),
                const SizedBox(height: 16),
                FilledButton.icon(
                  key: const Key('selected-place-new-journey'),
                  onPressed: c.busy || c.placeName == null || c.error != null
                      ? null
                      : () => unawaited(c.choose()),
                  icon: const Icon(Icons.add),
                  label: const Text('新建旅程草稿'),
                ),
                if (c.error != null)
                  OutlinedButton(
                    onPressed: c.busy ? null : () => unawaited(c.load()),
                    child: const Text('重新读取'),
                  ),
                TextButton(
                  onPressed: c.busy ? null : () => Navigator.maybePop(context),
                  child: const Text('取消，返回对话'),
                ),
              ],
            ],
          ),
        ),
      );
    },
  );
}
