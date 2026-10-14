import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:crypto/crypto.dart';

import 'business_console_page.dart';
import 'map_media_controller.dart';

import 'package:flutter/material.dart';
import '../city/map_cluster_members_sheet.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/scheduler.dart';
import 'package:http/http.dart' as http;

import '../app/birdtie_app_bar.dart';
import '../auth/birdtie_auth_controller.dart';
import '../auth/authenticated_client.dart';
import 'agent_relationship_page.dart';
import 'new_people_page.dart';
import 'opportunity_page.dart';
import 'social_now_page.dart';
import '../city/public_city_controller.dart';
import '../content/private_moment_controller.dart';
import '../content/agent_seed_controller.dart';
import '../content/moment_image_picker.dart';
import '../content/moment_control.dart';
import '../content/private_moment_media_controller.dart';
import '../content/personal_collection_controller.dart';
import '../content/video_material_controller.dart';
import '../legacy/legacy_shell.dart';
import 'agent_composer.dart';
import 'activity_plans.dart';
import 'activity_participations.dart';
import 'activity_detail_sheet.dart';
import 'activity_analytics.dart';
import 'connections.dart';
import 'contact_add_page.dart';
import 'chat_entity_router.dart';
import 'agent_result_sheet.dart';
import 'agent_activity_join_entry.dart';
import 'agent_activity_join_page.dart';
import 'agent_workspace_controller.dart';
import 'task_image_controller.dart';
import 'task_image_pending_store.dart';
import 'task_image_page.dart';
import 'agent_visual_controller.dart';
import 'agent_visual_page.dart';
import 'system_push_controller.dart';
import 'agent_request_failure.dart';
import 'entity_peek_card.dart';
import 'entity_action_contract.dart';
import 'entity_action_dispatcher.dart';
import 'inbox.dart';
import 'community_page.dart';
import 'community_api.dart';
import 'public_person_page.dart';
import 'public_moment_page.dart';
import 'map_canvas.dart';
import 'map_entities.dart';
import 'agent_entity_result_router.dart';
import 'map_layers_api.dart';
import 'map_layers_controller.dart';
import 'map_layer_controls.dart';
import 'now_public_place_panel.dart';
import 'active_social_intent_card.dart';
import 'active_social_intent_page.dart';
import 'place_detail_sheet.dart';
import 'now_discovery_controller.dart';
import 'remote_agent_task_source.dart';
import 'now_context_query_api.dart';
import 'now_context_query_controller.dart';
import 'now_context_selection_api.dart';
import 'now_context_selection_page.dart';
import 'online_social_opportunity_page.dart';
import 'saved_items.dart';
import 'saved_map_projection.dart';
import '../content/saved_collection_append.dart';
import 'entity_share_pending_store.dart' show chatEntityUUID;
import 'settings_page.dart';
import 'social_intent_drafts.dart';
import 'organization_workspaces.dart';
import 'notification_destination_router.dart';
import 'organization_membership_pages.dart';
import 'organization_map_location_page.dart';
import 'organization_console.dart';
import 'aberdeen_schedule.dart';
import 'organization_activity_api.dart';
import 'public_organization_page.dart';
import 'public_business_page.dart';
import 'sidebar.dart';
import 'top_controls.dart';
import 'now_workspace_layout.dart';
import 'now_ui_tokens.dart';
import 'now_perf_probe.dart';
import 'now_glass_surface.dart';
import 'now_social_edge.dart';
import 'now_social_edge_source.dart';
import 'now_composer_draft_state.dart';
import 'now_creation_draft.dart';
import 'now_creation_panel.dart';
import 'local_draft_context_page.dart';
import 'local_draft_media_bridge.dart';
import 'local_visual_draft.dart';
import 'route_draft.dart';
import 'route_draft_page.dart';
import 'friend_conversation_controller.dart';
import 'friend_conversation_thread.dart';
import 'friend_audio.dart';
import 'friend_image.dart';
import 'local_image_draft.dart';
import 'chat_message_pending_store.dart';
import 'remote_inbox_source.dart';
import 'now_place_preview.dart';
import '../auth/birdtie_login_sheet.dart';
import 'agent_result_projection.dart';
import 'external_place_candidates.dart';
import 'place_fact_presentation.dart';
import 'package:url_launcher/url_launcher.dart';
import '../config/birdtie_environment.dart';
import 'public_moment_photo.dart';
import 'public_moment_photo_controller.dart';
import 'public_moment_photo_editor.dart';
import 'public_moment_photo_pending_store.dart';

const _sidebarTapTraceEnabled = bool.fromEnvironment(
  'BIRDTIE_SIDEBAR_TAP_TRACE',
  defaultValue: false,
);
const _friendImageRolloutEnabled = bool.fromEnvironment(
  'BIRDTIE_FRIEND_IMAGE_ENABLED',
  defaultValue: false,
);

/// Transfer only the already selected local pixels into the existing private
/// Moment media action; never reopen the gallery or reuse an upload approval.
class _NowSelectedImagePicker extends MomentImagePicker {
  _NowSelectedImagePicker(this._bytes);
  Uint8List? _bytes;
  @override
  Future<MomentImageSelection?> selectSource(MomentImageSource source) async {
    final bytes = _bytes;
    _bytes = null;
    return bytes == null ? null : MomentImageSelection(bytes);
  }
}

class MapWorkspace extends StatefulWidget {
  const MapWorkspace({
    super.key,
    required this.city,
    required this.auth,
    required this.moments,
    this.agentTaskSource,
    this.connectionSource,
    this.seedClient,
    this.seedApiBaseUrl,
    this.placeDetailClient,
    this.placeDetailApiBaseUrl,
    this.taskImagePicker,
    this.taskImagePendingStore,
    this.friendAudioPendingStore,
    this.humanMessagePendingStore,
    this.friendImageEnabled = _friendImageRolloutEnabled,
    this.friendImagePicker,
    this.friendImagePendingStore,
  });
  final PublicCityController city;
  final BirdtieAuthController auth;
  final PrivateMomentController moments;
  final AgentTaskSource? agentTaskSource;
  final ConnectionSource? connectionSource;
  final http.Client? seedClient;
  final String? seedApiBaseUrl;
  final http.Client? placeDetailClient;
  final String? placeDetailApiBaseUrl;
  final MomentImagePicker? taskImagePicker;
  final TaskImagePendingStore? taskImagePendingStore;
  final FriendAudioPendingStore? friendAudioPendingStore;
  final HumanMessagePendingStore? humanMessagePendingStore;
  // Explicit matched-rollout gate: current API03 has no image route.
  final bool friendImageEnabled;
  final MomentImagePicker? friendImagePicker;
  final FriendImagePendingStore? friendImagePendingStore;

  @override
  State<MapWorkspace> createState() => _MapWorkspaceState();
}

class _MapWorkspaceState extends State<MapWorkspace>
    with WidgetsBindingObserver {
  final _scaffold = GlobalKey<ScaffoldState>();
  final _composerKey = GlobalKey<AgentComposerState>();
  final _composerDrafts = NowComposerDraftState();
  NowCreationDraft? _creation;
  Object? _creationScope;
  bool _creationCollapsed = false, _creationBusy = false;
  bool _creationTitleEditing = false, _creationUncertain = false;
  bool _creationPublished = false;
  String? _creationError,
      _creationNotice,
      _creationMomentID,
      _creationOperationID,
      _creationActivityID;
  MomentControlPreview? _creationSavePreview;
  MomentControlPreview? _creationPublishPreview;
  PrivateMomentMediaController? _creationImage;
  PublicMomentPhotoController? _creationPublicPhoto;
  Uint8List? _creationPublicPhotoPixels;
  int? _creationPublishedRevision;
  bool _creationPhotoPublished = false;
  PersonalCollectionController? _creationCollection;
  final _creationPlaces = <CollectionPlace>[];
  final _retainedCreations = <Object, NowCreationDraft>{};
  TextEditingValue? _beforeCreationInput;

  Object get _creationIdentity => (
    widget.auth,
    widget.auth.accountID,
    widget.auth.authorizationHeader,
    widget.city,
    widget.city.selectedCity?.id,
    _organizations.active?.id,
    _seedEntryEpoch,
    _personalSourceSerial,
    widget.seedClient,
    widget.seedApiBaseUrl,
    _composerDrafts.target.key,
  );
  Object _creationLocalKey(String kind) => (
    widget.seedApiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
    widget.auth.accountID,
    widget.city.selectedCity?.id,
    kind,
  );
  bool get _creationCurrent =>
      _creation != null &&
      _creationScope == _creationIdentity &&
      widget.auth.signedIn &&
      _organizations.active == null &&
      !_composerDrafts.target.human;

  void _retireCreationApprovals() {
    _creationSavePreview = null;
    _creationPublishPreview = null;
    widget.moments.retirePreview();
    _creationImage?.dispose();
    _creationImage = null;
  }

  Future<void> _beginCreation(String kind) async {
    if (_creation != null && !_creationCurrent) _clearRetiredCreation();
    if (_composerDrafts.target.human || _creationBusy) return;
    if (!widget.auth.signedIn) await _signInHere();
    if (!mounted || !widget.auth.signedIn || _composerDrafts.target.human)
      return;
    if (_organizations.active != null) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请切回个人身份后创建。')));
      return;
    }
    final city = widget.city.selectedCity;
    if (city == null) {
      await _chooseCity();
      return;
    }
    if (_creationCurrent && _creation!.kind == kind) {
      setState(() => _creationCollapsed = false);
      return;
    }
    if (_creationCurrent) {
      _retainedCreations[_creationLocalKey(_creation!.kind)] = _creation!;
    }
    final text = _composerDrafts.value.text;
    _beforeCreationInput ??= _composerDrafts.value;
    _retireCreationApprovals();
    _disposeCreationPublicPhoto();
    _creationCollection?.dispose();
    _creationCollection = null;
    _creationPlaces.clear();
    var draft =
        _retainedCreations[_creationLocalKey(kind)] ??
        NowCreationDraft(kind: kind, cityID: city.id, cityName: city.name);
    if (draft.body.isEmpty && text.trim().isNotEmpty)
      draft = draft.withText(text);
    setState(() {
      _creation = draft;
      _creationScope = _creationIdentity;
      _creationCollapsed = false;
      _creationTitleEditing = false;
      _creationError = _creationNotice = _creationMomentID =
          _creationActivityID = _creationOperationID = null;
      _creationUncertain = false;
      _creationPublished = false;
      _creationPublishedRevision = null;
      _creationPhotoPublished = false;
      _socialExpanded = false;
      _exploreExpanded = false;
      _retirePublicPlacePanel();
    });
    _composerDrafts.update(TextEditingValue.empty);
  }

  void _exitCreation() {
    if (_creationBusy) return;
    if (_creationCurrent) {
      _retainedCreations[_creationLocalKey(_creation!.kind)] = _creation!;
    }
    _retireCreationApprovals();
    _disposeCreationPublicPhoto();
    setState(() {
      _creation = null;
      _creationScope = null;
    });
    _composerDrafts.update(_beforeCreationInput ?? TextEditingValue.empty);
    _beforeCreationInput = null;
  }

  void _clearRetiredCreation() {
    _retireCreationApprovals();
    _disposeCreationPublicPhoto();
    _creationCollection?.dispose();
    _creationCollection = null;
    _creation = null;
    _creationScope = null;
    _creationBusy = _creationUncertain = _creationPublished = false;
    _creationMomentID = _creationActivityID = null;
    _creationOperationID = null;
    _beforeCreationInput = null;
    _creationPlaces.clear();
  }

  void _updateCreation(String text) {
    if (!_creationCurrent || _creationBusy || _creationUncertain) return;
    if (_creationPublished) {
      setState(() => _creationError = '这条动态已经发布，请先点“写一条新动态”。');
      return;
    }
    _retireCreationApprovals();
    setState(() {
      _creation = _creation!.withText(text, titleOnly: _creationTitleEditing);
      _creationTitleEditing = false;
      _creationCollapsed = false;
      _creationError = _creationNotice = null;
      _retainedCreations[_creationLocalKey(_creation!.kind)] = _creation!;
    });
    _composerDrafts.update(TextEditingValue.empty);
  }

  void _editCreationText(bool title) {
    if (!_creationCurrent || _creationBusy || _creationUncertain) return;
    setState(() => _creationTitleEditing = title);
    _composerKey.currentState?.fillDraft(
      title ? _creation!.title : _creation!.body,
    );
  }

  Future<void> _chooseCreationPlace() async {
    if (!_creationCurrent || _creationBusy || _creationUncertain) return;
    final scope = _creationScope;
    final selected = await showModalBottomSheet<CollectionPlace>(
      context: context,
      useSafeArea: true,
      showDragHandle: true,
      isScrollControlled: true,
      builder: (sheet) => SizedBox(
        height: MediaQuery.sizeOf(sheet).height * .55,
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.all(16),
              child: Text(
                _creation!.kind == 'moment' ? '关联地点' : '添加地点',
                style: NowUiTokens.of(sheet).sheetTitle,
              ),
            ),
            Expanded(
              child: widget.city.places.isEmpty
                  ? const Center(child: Text('当前城市暂无可选地点。'))
                  : ListView(
                      children: [
                        for (final place in widget.city.places)
                          ListTile(
                            title: Text(place.name),
                            onTap: () {
                              if (_creationCurrent && scope == _creationScope) {
                                Navigator.pop(
                                  sheet,
                                  CollectionPlace(
                                    id: place.id,
                                    name: place.name,
                                  ),
                                );
                              }
                            },
                          ),
                      ],
                    ),
            ),
          ],
        ),
      ),
    );
    if (!_creationCurrent || scope != _creationScope || selected == null)
      return;
    if (_creationPublished) {
      setState(() => _creationError = '这条动态已经发布，请先点“写一条新动态”。');
      return;
    }
    _retireCreationApprovals();
    setState(() {
      _creation = _creation!.copyWith(
        placeID: selected.id,
        placeName: selected.name,
      );
      if (!_creationPlaces.any((p) => p.id == selected.id))
        _creationPlaces.add(selected);
      _creationError = _creationNotice = null;
    });
  }

  Future<void> _chooseCreationTime() async {
    if (!_creationCurrent || _creationBusy || _creationUncertain) return;
    final scope = _creationScope;
    // Reuse the existing city conversion; never interpret device time as UK time.
    if (_creation!.cityID != 'aberdeen-gb') {
      setState(() => _creationError = '当前活动时间仅支持阿伯丁，请先切换城市。');
      return;
    }
    Future<DateTime?> choose(String label) async {
      final day = await showDatePicker(
        context: context,
        helpText: '$label日期（阿伯丁时间）',
        initialDate: DateTime.now(),
        firstDate: DateTime.now().subtract(const Duration(days: 1)),
        lastDate: DateTime.now().add(const Duration(days: 730)),
      );
      if (!mounted ||
          !_creationCurrent ||
          scope != _creationScope ||
          day == null)
        return null;
      final time = await showTimePicker(
        context: context,
        helpText: '$label时间（阿伯丁时间）',
        initialTime: TimeOfDay.now(),
      );
      if (time == null || !_creationCurrent || scope != _creationScope)
        return null;
      return aberdeenLocalToUtc(day, time.hour, time.minute);
    }

    final start = await choose('开始');
    if (start == null ||
        !mounted ||
        !_creationCurrent ||
        scope != _creationScope)
      return;
    final end = await choose('结束');
    if (end == null || !mounted || !_creationCurrent || scope != _creationScope)
      return;
    if (!end.isAfter(start) ||
        end.difference(start) > const Duration(days: 7)) {
      setState(() => _creationError = '结束须晚于开始，活动时长不能超过 7 天。');
      return;
    }
    _retireCreationApprovals();
    setState(() {
      _creation = _creation!.copyWith(startsAt: start, endsAt: end);
      _creationError = _creationNotice = null;
    });
  }

  Future<void> _saveCreation() async {
    if (!_creationCurrent || _creationBusy || _creationUncertain) return;
    if (_creationPublished) return;
    if (_composerDrafts.value.text.trim().isNotEmpty) {
      _updateCreation(_composerDrafts.value.text);
    }
    final draft = _creation!, scope = _creationScope;
    bool current() =>
        mounted &&
        _creationCurrent &&
        scope == _creationScope &&
        identical(draft, _creation);
    if (draft.title.trim().isEmpty ||
        utf8.encode(draft.title.trim()).length > 160 ||
        utf8.encode(draft.body).length > 5000) {
      setState(() => _creationError = '先在下方写下内容；名称或正文较长时，请缩短后保存。');
      return;
    }
    setState(() {
      _creationBusy = true;
      _creationError = null;
    });
    try {
      if (draft.kind == 'moment') {
        final original = widget.moments.moments
            .where((m) => m.id == _creationMomentID)
            .firstOrNull;
        if (_creationMomentID != null && original == null) {
          setState(() => _creationError = '原草稿当前不可读取，请先到个人记录核实。');
          return;
        }
        final p = original == null
            ? await widget.moments.previewCreate(
                cityID: draft.cityID,
                title: draft.title,
                body: draft.body,
                placeID: draft.placeID,
                current: current,
              )
            : await widget.moments.previewUpdate(
                moment: original,
                title: draft.title,
                body: draft.body,
                placeID: draft.placeID,
                current: current,
              );
        if (current())
          setState(() {
            _creationSavePreview = p;
            _creationError = p == null
                ? widget.moments.operationError ?? '暂时无法检查草稿，请重试。'
                : null;
            _creationNotice = p == null
                ? null
                : '已检查这版文字草稿，仅自己可见。确认后保存；已选媒体继续保留在输入框。';
          });
      } else if (draft.kind == 'activity') {
        if (draft.startsAt == null || draft.endsAt == null) {
          if (current())
            setState(() => _creationError = '还缺开始和结束时间，请点“选择活动时间”。');
          return;
        }
        final api = OrganizationActivityApi(
          authorizationHeader: () =>
              current() ? widget.auth.authorizationHeader : null,
          client: _authenticatedTransport(widget.seedClient),
          apiBaseUrl: widget.seedApiBaseUrl,
        );
        try {
          final saved = await api.saveAs(
            ActivityOrganizer(
              type: 'PERSON',
              id: widget.auth.accountID!,
              name: '我自己',
            ),
            {
              'cityId': draft.cityID,
              ...activityLocationFields(
                draft.placeID.isEmpty ? 'tbd' : 'confirmed',
                draft.placeID,
              ),
              'title': draft.title,
              'summary': String.fromCharCodes(draft.body.runes.take(300)),
              'description': draft.body,
              'startsAt': draft.startsAt!.toIso8601String(),
              'endsAt': draft.endsAt!.toIso8601String(),
              'timeZone': widget.city.selectedCity!.timeZone,
              'capacity': null,
              'priceMinor': 0,
              'currency': '',
              'eligibility': '',
              'languageCode': '',
              'visibility': 'private',
            },
            activityId: _creationActivityID,
          );
          if (current())
            setState(() {
              _creationActivityID = saved.id;
              _creationNotice = '活动草稿已保存，仅自己可见。';
            });
          if (current() && saved.id.isEmpty) throw const FormatException();
        } on OrganizationActivityException catch (error) {
          if (current())
            setState(() {
              _creationUncertain = error.status >= 500 || error.status == 408;
              _creationError = _creationUncertain
                  ? '活动保存结果待核实，草稿仍在；请先到 Plans 检查，避免重复创建。'
                  : error.chineseMessage;
            });
        } catch (_) {
          if (current())
            setState(() {
              _creationUncertain = true;
              _creationError = '活动保存结果待核实，草稿仍在；请先到 Plans 检查，避免重复创建。';
            });
        } finally {
          api.dispose();
        }
      } else {
        if (_creationPlaces.isEmpty) {
          if (current()) setState(() => _creationError = '还缺地点，请添加至少一个地点。');
          return;
        }
        final c = _creationCollection ??= PersonalCollectionController(
          kind: draft.kind,
          cityID: draft.cityID,
          authorizationHeader: () =>
              _creationCurrent ? widget.auth.authorizationHeader : null,
          ownerID: () => _creationCurrent ? widget.auth.accountID : null,
          sourceCurrent: () =>
              mounted && _creationCurrent && scope == _creationScope,
          identityChanges: _activeIntentChanges,
          client: _authenticatedTransport(widget.seedClient),
          apiBaseUrl: widget.seedApiBaseUrl,
        );
        c.setText(title: draft.title, summary: draft.body);
        // Keep the same canonical controller for edits and uncertain retry.
        for (var i = c.draftEntries.length - 1; i >= 0; i--) {
          if (!_creationPlaces.any((p) => p.id == c.draftEntries[i].placeID))
            c.removeEntry(i);
        }
        for (final place in _creationPlaces) c.addPlace(place);
        final saved = await c.save();
        if (current())
          setState(() {
            _creationNotice = saved ? '${draft.label}草稿已保存，仅自己可见。' : null;
            _creationError = saved ? null : c.error;
            _creationUncertain = c.uncertain;
          });
      }
    } finally {
      if (current()) setState(() => _creationBusy = false);
    }
  }

  Future<void> _confirmCreationSave() async {
    final p = _creationSavePreview;
    if (!_creationCurrent || _creationBusy || p == null) return;
    final scope = _creationScope, draft = _creation;
    _creationOperationID = p.input.operationID;
    bool current() =>
        mounted &&
        _creationCurrent &&
        scope == _creationScope &&
        identical(draft, _creation);
    setState(() => _creationBusy = true);
    final saved = await widget.moments.commit(p, current: current);
    if (!current()) return;
    setState(() {
      _creationBusy = false;
      _creationSavePreview = null;
      _creationMomentID = saved
          ? widget.moments.operationReceipt?.momentID
          : null;
      _creationNotice = saved ? '动态文字草稿已保存，仅自己可见。' : null;
      _creationError = saved ? null : widget.moments.operationError;
      _creationUncertain = widget.moments.creationUncertain;
    });
    if (saved && _imageDraft?.localBytes != null)
      await _prepareCreationLocalImage();
  }

  Future<void> _prepareCreationLocalImage() async {
    final pixels = _imageDraft?.localBytes, id = _creationMomentID;
    final moment = widget.moments.moments.where((m) => m.id == id).firstOrNull;
    if (!_creationCurrent || pixels == null || id == null || moment == null)
      return;
    final scope = _creationScope, selected = _localImageSelectionSerial;
    final owner = widget.auth.accountID;
    _creationImage?.dispose();
    final c = PrivateMomentMediaController(
      momentID: id,
      momentRevision: moment.revision,
      authorizationHeader: () => widget.auth.authorizationHeader,
      ownerID: () => widget.auth.accountID,
      identityChanges: _activeIntentChanges,
      sourceCurrent: () =>
          mounted &&
          _creationCurrent &&
          scope == _creationScope &&
          selected == _localImageSelectionSerial &&
          widget.auth.accountID == owner &&
          _imageDraft?.localBytes != null,
      organizationWorkspaceID: () => _organizations.active?.id,
      apiBaseUrl: widget.seedApiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
      client: _authenticatedTransport(widget.seedClient),
      picker: _NowSelectedImagePicker(Uint8List.fromList(pixels)),
    );
    _creationImage = c;
    await c.select();
    if (mounted && _creationCurrent && identical(c, _creationImage))
      setState(() {
        _creationError = c.error;
        if (c.hasLocalImage) _creationNotice = '文字草稿已保存。下方这张图片待确认加入该动态，仅自己可见。';
      });
  }

  Future<void> _saveCreationImage() async {
    final c = _creationImage;
    if (!_creationCurrent || _creationBusy || c == null || c.retired) return;
    final scope = _creationScope;
    setState(() => _creationBusy = true);
    if (c.unknown) {
      await c.reconcile();
    } else if (c.preview == null) {
      c.acknowledgeLocal(true);
      await c.prepare();
    } else {
      await c.saveReviewed(confirmed: true);
    }
    if (mounted &&
        _creationCurrent &&
        scope == _creationScope &&
        identical(c, _creationImage))
      setState(() {
        _creationBusy = false;
        _creationError = c.error;
        _creationNotice =
            c.images.any((image) => image.status == 'ready_private')
            ? '图片已保存到这条私人动态。'
            : c.preview != null
            ? '确认后将这张图片保存到该动态，仅自己可见。'
            : c.message;
      });
  }

  Future<void> _publishCreation() async {
    if (!_creationCurrent || _creationBusy || _creationMomentID == null) return;
    if (_creation!.placeID.isEmpty) {
      setState(() => _creationError = '公开动态还需关联一个真实地点。');
      return;
    }
    if (_videoMaterials?.selection != null) {
      setState(() => _creationError = '视频暂不能公开到动态。请保留视频或移除后再检查文字发布。');
      return;
    }
    if (_creationImage?.unknown == true || _creationImage?.busy == true) {
      setState(() => _creationError = '请先核实原私人图片保存操作，再公开这条动态。');
      return;
    }
    final scope = _creationScope;
    final c = widget.moments;
    final draft = _creation!;
    bool current() =>
        mounted &&
        _creationCurrent &&
        scope == _creationScope &&
        identical(draft, _creation);
    setState(() => _creationBusy = true);
    try {
      final m = await c.readOwned(_creationMomentID!, current: current);
      if (current() && m != null) {
        if (m.cityID != draft.cityID ||
            m.placeID != draft.placeID ||
            m.title != draft.title.trim() ||
            m.body != draft.body.trim()) {
          _creationError = '当前内容还没有保存。请先确认保存这版草稿，再检查公开发布。';
        } else {
          _creationError = null;
          _creationPublishPreview = await c.previewPublish(m, current: current);
        }
      }
    } catch (_) {
      if (current()) _creationError = '当前草稿暂不可核实，文字已保留。';
    }
    if (mounted && _creationCurrent && scope == _creationScope)
      setState(() {
        _creationBusy = false;
        _creationError = c.operationError ?? _creationError;
        _creationNotice = _creationPublishPreview == null
            ? null
            : _imageDraft?.localBytes != null
            ? '本次先公开这版文字。所选图片仍未公开，随后需另行确认图片权利和公开结果。'
            : '公开后，所有人可在关联地点查看这版文字。';
      });
  }

  Future<void> _confirmCreationPublication() async {
    final c = widget.moments, p = _creationPublishPreview;
    if (!_creationCurrent || _creationBusy || p == null) return;
    final scope = _creationScope;
    _creationOperationID = p.input.operationID;
    setState(() => _creationBusy = true);
    final saved = await c.commit(
      p,
      current: () => mounted && _creationCurrent && scope == _creationScope,
    );
    if (mounted && _creationCurrent && scope == _creationScope)
      setState(() {
        _creationBusy = false;
        _creationError = c.operationError;
        _creationUncertain = c.recoveryOperations.isNotEmpty;
        _creationPublishPreview = null;
        _creationNotice = saved ? '这条文字动态已公开发布。' : null;
        _creationPublished = saved;
        if (saved)
          _creationPublishedRevision = c.operationReceipt?.appliedRevision;
      });
    if (saved && _creationCurrent) {
      _creationImage?.dispose();
      _creationImage = null;
      _syncCreationPublicPhoto();
    }
    if (saved && mounted && _layers.cityID != null && _layers.bounds != null) {
      unawaited(_layers.load(_layers.cityID!, _layers.bounds!));
    }
  }

  void _disposeCreationPublicPhoto() {
    _creationPublicPhoto?.dispose();
    _creationPublicPhoto = null;
    _creationPublicPhotoPixels = null;
    _creationPhotoPrepared = null;
    _creationPhotoResolved = null;
  }

  void _syncCreationPublicPhoto() {
    final local = _imageDraft, pixels = _imageDraft?.localBytes;
    final id = _creationMomentID, revision = _creationPublishedRevision;
    if (!_creationCurrent ||
        !_creationPublished ||
        _creationPhotoPublished ||
        id == null ||
        revision == null ||
        local == null ||
        !local.current ||
        pixels == null)
      return;
    if (_creationPublicPhoto?.current == true) return;
    _disposeCreationPublicPhoto();
    final scope = _creationScope, owner = widget.auth.accountID;
    final selected = _localImageSelectionSerial,
        localRevision = local.localRevision;
    final digest = sha256.convert(pixels).toString();
    final moments = widget.moments;
    bool current() =>
        mounted &&
        _creationCurrent &&
        _creationPublished &&
        scope == _creationScope &&
        owner == widget.auth.accountID &&
        identical(moments, widget.moments) &&
        id == _creationMomentID &&
        revision == _creationPublishedRevision &&
        identical(local, _imageDraft) &&
        local.current &&
        local.localRevision == localRevision &&
        selected == _localImageSelectionSerial &&
        local.localBytes != null &&
        sha256.convert(local.localBytes!).toString() == digest;
    final c = PublicMomentPhotoController(
      momentID: id,
      api: PublicMomentPhotoAPI(
        client: moments.borrowedClient,
        baseUrl: moments.apiBaseUrl,
        authorizationHeader: () => widget.auth.authorizationHeader,
        current: current,
      ),
      ownerID: () => widget.auth.accountID,
      workspaceID: () => _organizations.active?.id,
      identityChanges: _activeIntentChanges,
      sourceKey: () => (scope, owner, id, revision, current()),
      sourceCurrent: current,
    );
    _creationPublicPhotoPixels = Uint8List.fromList(pixels);
    _creationPublicPhoto = c;
    final prepared = <String, PublicMomentPhotoPending>{};
    _creationPhotoPrepared = (p) {
      if (current() &&
          identical(c, _creationPublicPhoto) &&
          p.input.action == 'SAVE' &&
          p.input.expectedMomentRevision == revision) {
        prepared[p.input.operationID] = PublicMomentPhotoPending.preview(p);
      }
    };
    // Cleanup is tied to the exact pixels and original SAVE receipt. A restored
    // journal has no such RAM binding and never removes a new local selection.
    _creationPhotoResolved = (p, r) {
      if (!current() ||
          !identical(c, _creationPublicPhoto) ||
          p.action != 'SAVE' ||
          p.momentID != id ||
          !r.succeeded ||
          p.operationID != r.operationID ||
          p.expectedMomentRevision != revision ||
          prepared[p.operationID]?.same(p) != true)
        return;
      _creationPhotoPublished = true;
      local.removeLocal();
      _disposeCreationPublicPhoto();
      setState(() => _creationNotice = '图片公开操作已完成，可打开动态查看当前图片。');
      _queueLocalMediaSync();
    };
    setState(() => _creationNotice = '文字已公开。图片仍未公开，请在下方检查并单独确认。');
  }

  void Function(PublicMomentPhotoPending, PublicMomentPhotoReceipt)?
  _creationPhotoResolved;
  ValueChanged<PublicMomentPhotoPreview>? _creationPhotoPrepared;

  Widget? _creationVideoPreview() {
    final c = _videoMaterials, selection = _videoMaterials?.selection;
    if (c == null || !c.current || selection == null) return null;
    return Row(
      children: [
        SizedBox(
          width: 64,
          height: 56,
          child: ClipRRect(
            borderRadius: BorderRadius.circular(8),
            child: FittedBox(
              fit: BoxFit.contain,
              child: SizedBox(
                width: selection.preview.width.toDouble(),
                height: selection.preview.height.toDouble(),
                child: selection.preview.view(),
              ),
            ),
          ),
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            '视频 · ${(selection.durationMS / 1000).ceil()} 秒 · 已添加到当前输入',
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
          ),
        ),
        IconButton(
          tooltip: selection.preview.playing ? '暂停视频预览' : '播放视频预览',
          onPressed: c.busy
              ? null
              : () async {
                  await selection.preview.toggle();
                  if (mounted && c.current) setState(() {});
                },
          icon: Icon(
            selection.preview.playing ? Icons.pause : Icons.play_arrow,
          ),
        ),
        IconButton(
          tooltip: '移除本机视频',
          onPressed: c.busy ? null : () => unawaited(c.removeLocal()),
          icon: const Icon(Icons.close),
        ),
      ],
    );
  }

  Widget _creationPanel() {
    final draft = _creation!;
    final photo = _creationPublicPhoto;
    return NowCreationPanel(
      draft: draft,
      collapsed: _creationCollapsed,
      busy: _creationBusy,
      saveEnabled: !_creationUncertain && !_creationPublished,
      editingEnabled: !_creationUncertain && !_creationPublished,
      notice: _creationNotice,
      error: _creationError,
      statusLabel: _creationPublished
          ? _creationPhotoPublished
                ? '文字已公开 · 图片操作已完成'
                : _imageDraft?.localBytes != null
                ? '文字已公开 · 图片待确认'
                : '文字已公开'
          : (_creationMomentID != null ||
                _creationActivityID != null ||
                _creationCollection?.editing != null)
          ? '已保存草稿 · 仅自己可见'
          : '本地草稿 · 仅自己可见',
      onToggle: () => setState(() => _creationCollapsed = !_creationCollapsed),
      onClose: _exitCreation,
      onEditTitle: () => _editCreationText(true),
      onEditBody: () => _editCreationText(false),
      onChoosePlace: () => unawaited(_chooseCreationPlace()),
      onChooseTime: () => unawaited(_chooseCreationTime()),
      onSave: () => unawaited(_saveCreation()),
      onPublish:
          draft.kind == 'moment' &&
              !_creationPublished &&
              _creationMomentID != null &&
              !_creationUncertain
          ? () => unawaited(_publishCreation())
          : null,
      onRecover: _creationUncertain && draft.kind == 'moment'
          ? () async {
              final c = widget.moments,
                  scope = _creationScope,
                  op = _creationOperationID;
              final ownerEpoch = c.logicalIdentityEpoch;
              bool current() =>
                  mounted &&
                  _creationCurrent &&
                  scope == _creationScope &&
                  identical(c, widget.moments) &&
                  ownerEpoch == c.logicalIdentityEpoch &&
                  op == _creationOperationID;
              if (op == null) {
                setState(() => _creationError = '请到我的动态核实原操作，本页面不会猜测操作归属。');
                return;
              }
              await c.loadOperations();
              if (!current()) return;
              for (final p in List.of(
                c.recoveryOperations.where((p) => p.operationID == op),
              )) {
                if (!current()) return;
                await c.recoverOperation(p);
                if (!current()) return;
              }
              if (current() && c.operationReceipt?.operationID == op)
                setState(() {
                  _creationUncertain =
                      widget.moments.recoveryOperations.isNotEmpty;
                  _creationError = widget.moments.operationError;
                  if (widget.moments.operationReceipt?.succeeded == true) {
                    _creationMomentID =
                        widget.moments.operationReceipt!.momentID;
                    _creationPublished =
                        widget.moments.operationReceipt!.action == 'PUBLISH';
                    if (_creationPublished)
                      _creationPublishedRevision =
                          widget.moments.operationReceipt!.appliedRevision;
                    _creationNotice = _creationPublished
                        ? '原公开发布结果已核实。'
                        : '原动态保存结果已核实。';
                  }
                });
              if (current() && _creationPublished) _syncCreationPublicPhoto();
            }
          : _creationUncertain &&
                _creationCollection != null &&
                _creationCollection!.editing == null
          ? () async {
              await _creationCollection!.recover();
              if (mounted && _creationCurrent)
                setState(() {
                  _creationUncertain = _creationCollection!.uncertain;
                  _creationError = _creationCollection!.error;
                });
            }
          : null,
      extraActions: [
        if (_creationImage?.hasLocalImage == true &&
            _creationImage!.images.isEmpty)
          TextButton(
            onPressed: _creationBusy
                ? null
                : () => unawaited(_saveCreationImage()),
            child: Text(
              _creationImage!.unknown
                  ? '核实图片保存结果'
                  : _creationImage!.preview == null
                  ? '检查加入这张图片'
                  : '确认保存这张图片',
            ),
          ),
        if (_creationPublished)
          TextButton(
            onPressed: () => unawaited(_viewCreationMoment()),
            child: const Text('查看这条动态'),
          ),
        if (_creationPublished)
          TextButton(
            onPressed: () {
              final key = _creationLocalKey('moment');
              _exitCreation();
              _retainedCreations.remove(key);
              _composerDrafts.update(TextEditingValue.empty);
              unawaited(_beginCreation('moment'));
            },
            child: const Text('写一条新动态'),
          ),
        if (draft.kind == 'journey' || draft.kind == 'theme_map')
          for (final place in _creationPlaces)
            InputChip(
              label: Text(place.name),
              onDeleted: _creationBusy
                  ? null
                  : () {
                      setState(() => _creationPlaces.remove(place));
                    },
            ),
        if (_creationSavePreview != null)
          FilledButton(
            onPressed: _creationBusy
                ? null
                : () => unawaited(_confirmCreationSave()),
            child: const Text('确认保存这版文字草稿'),
          ),
        if (_creationPublishPreview != null &&
            widget.moments.previewCurrent(_creationPublishPreview!))
          FilledButton(
            onPressed: _creationBusy
                ? null
                : () => unawaited(_confirmCreationPublication()),
            child: Text(
              _imageDraft?.localBytes != null ? '确认先公开这版文字' : '确认公开发布这版文字',
            ),
          ),
      ],
      extraContent: photo != null
          ? PublicMomentPhotoEditor(
              key: ObjectKey(photo),
              controller: photo,
              embedded: true,
              ownsController: false,
              allowImageSelection: false,
              initialImage: _creationPublicPhotoPixels,
              momentTitle: draft.title,
              momentTitleRevision: _creationPublishedRevision,
              onResolved: _creationPhotoResolved,
              onPrepared: _creationPhotoPrepared,
            )
          : null,
    );
  }

  Future<void> _viewCreationMoment() async {
    final id = _creationMomentID;
    if (!_creationCurrent || !_creationPublished || id == null) return;
    _pauseComposer();
    final epoch = _seedEntryEpoch, auth = widget.auth, moments = widget.moments;
    final momentEpoch = _observeMomentEntrySource();
    bool current() =>
        mounted &&
        auth == widget.auth &&
        epoch == _seedEntryEpoch &&
        identical(moments, widget.moments) &&
        momentEpoch == _observeMomentEntrySource() &&
        _organizations.active == null;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: _activeIntentChanges,
          current: current,
          builder: (_) => PublicMomentPage(
            momentID: id,
            authorizationHeader: () =>
                current() ? auth.authorizationHeader : null,
            ownerID: () => current() ? auth.accountID : null,
            workspaceID: () => _organizations.active?.id,
            identityChanges: Listenable.merge([
              auth,
              _organizations,
              _seedEntryChanges,
            ]),
            client: moments.borrowedClient,
            apiBaseUrl: moments.apiBaseUrl,
          ),
        ),
      ),
    );
    if (current() && _layers.cityID != null && _layers.bounds != null)
      unawaited(_layers.load(_layers.cityID!, _layers.bounds!));
  }

  final _localMedia = LocalDraftMediaBridge();
  LocalDraftMediaBinding? _localMediaBinding;
  final _localMediaSelections = <String, LocalDraftMediaSelection>{};
  bool _localMediaSyncQueued = false;
  int _localImageSelectionSerial = 0;

  void _queueLocalMediaSync() {
    if (!mounted || _localMediaSyncQueued) return;
    _localMediaSyncQueued = true;
    scheduleMicrotask(() {
      _localMediaSyncQueued = false;
      if (mounted) _syncLocalMedia();
    });
  }

  void _syncLocalMedia() {
    final original = _composerDrafts.envelope;
    if (!mounted ||
        original == null ||
        _composerDrafts.suspended ||
        !widget.auth.signedIn ||
        original.owner != widget.auth.accountID) {
      _localMedia.retire();
      _localMediaBinding = null;
      _localMediaSelections.clear();
      return;
    }
    final binding = _localMedia.bind(
      LocalDraftMediaNamespace.fromDraft(original),
    );
    if (!identical(binding, _localMediaBinding)) _localMediaSelections.clear();
    _localMediaBinding = binding;
    final draftIdentity = _composerDrafts.identity;
    final human = _composerDrafts.target.human, friend = _friendConversation;
    final ComposerImageDraftView? image = human
        ? friend?.imageDraft
        : _imageDraft;
    final imageBytes = image?.current == true ? image?.localBytes : null;
    if (image != null && imageBytes != null) {
      final digest = sha256.convert(imageBytes).toString();
      _localMediaSelections['image'] = LocalDraftMediaSelection(
        identity: ('image', digest, human ? 0 : _localImageSelectionSerial),
        source: image,
        current: true,
        state: human && friend?.imagePending.isNotEmpty == true
            ? 'unknown'
            : 'local',
        sourceCurrent: () =>
            mounted &&
            image.current &&
            identical(
              image,
              human ? _friendConversation?.imageDraft : _imageDraft,
            ) &&
            identical(image.localBytes, imageBytes) &&
            _composerDrafts.identity == draftIdentity &&
            _localMedia.isCurrent(binding),
      );
    } else {
      _retireLocalMediaSlot('image');
    }
    final audio = human && friend?.current == true ? friend?.audioDraft : null;
    if (audio != null) {
      _localMediaSelections['audio'] = LocalDraftMediaSelection(
        identity: ('audio', audio.sha),
        source: audio,
        current: true,
        state: friend!.audioPending.isNotEmpty ? 'unknown' : 'local',
        sourceCurrent: () =>
            mounted &&
            friend.current &&
            identical(friend, _friendConversation) &&
            identical(audio, friend.audioDraft) &&
            _composerDrafts.identity == draftIdentity &&
            _localMedia.isCurrent(binding),
      );
    } else {
      _retireLocalMediaSlot('audio');
    }
    final video = !human ? _videoMaterials : null;
    final selection = video?.current == true ? video?.selection : null;
    if (video != null && selection != null) {
      _localMediaSelections['video'] = LocalDraftMediaSelection(
        identity: ('video', selection.sha256Hex),
        source: selection,
        current: true,
        state: video.uncertain
            ? 'unknown'
            : video.active?.status == 'ready_private'
            ? 'uploaded'
            : 'local',
        sourceCurrent: () =>
            mounted &&
            video.current &&
            identical(video, _videoMaterials) &&
            identical(selection, video.selection) &&
            !_composerDrafts.target.human &&
            _composerDrafts.identity == draftIdentity &&
            _localMedia.isCurrent(binding),
      );
    } else {
      _retireLocalMediaSlot('video');
    }
    var next = original;
    try {
      for (final entry in _localMediaSelections.entries) {
        next = _localMedia.sync(
          binding,
          next,
          kind: entry.key,
          selections: [entry.value],
        );
      }
      if (!identical(next, original)) {
        _composerDrafts.setContext(attachments: next.attachments);
      }
    } on FormatException {
      // The current material stays in its original editor. Existing history
      // must not be discarded to make room for a new selector.
    }
  }

  void _retireLocalMediaSlot(String kind) {
    final old = _localMediaSelections[kind];
    if (old == null) return;
    _localMediaSelections[kind] = LocalDraftMediaSelection(
      identity: old.identity,
      source: old.source,
      current: false,
      state: old.state == 'unknown' || old.state == 'uploaded'
          ? old.state
          : 'unavailable',
    );
  }

  String? _lastDraftStorageError;
  final _friendConversations = <String, FriendConversationController>{};
  final _retainedFriendAudio =
      <String, ({String owner, FriendAudioDraft draft})>{};
  final _retainedFriendImages =
      <
        String,
        ({String owner, String recipient, LocalImageRetainedDraft draft})
      >{};
  int _humanDraftRevision = 0;
  TaskImageRetainedDraft? _retainedImageDraft;
  // Retain routing selectors only. Private history must be read again under
  // the replacement credential, and recovery must never replay an Agent turn.
  AgentTask? _rejectedAgentTask;
  Future<void>? _loginFlow;
  late final AuthenticatedClient _privateClient;
  (String?, String?, String?)? _lastAuthIdentity;
  bool _needsLogin = false;
  String? _socialRequestScope;
  NowSocialSection _socialSection = NowSocialSection.messages;
  bool _socialAllConversations = false;
  int _socialMessageUnread = 0;
  int? _socialNotificationUnread;
  late final NowPerfProbe _nowPerf;
  bool _socialFollowedContacts = false;
  int _socialSelectionSerial = 0;
  FriendConversationController? get _friendConversation =>
      _friendConversations[_composerDrafts.target.conversationID];
  // Opt-in observation only: no key, clock, pointer subscription or surface
  // wrapper in the normal build. It never opens/retries or changes gestures.
  final GlobalKey? _sidebarTraceHeader = _sidebarTapTraceEnabled
      ? GlobalKey()
      : null;
  final Stopwatch? _sidebarTraceClock = _sidebarTapTraceEnabled
      ? (Stopwatch()..start())
      : null;
  final Set<int>? _sidebarTracePointers = _sidebarTapTraceEnabled ? {} : null;
  int _sidebarTraceSequence = 0;
  int _sidebarTraceDeliveryCount = 0;
  int? _sidebarTraceViewID;
  double? _sidebarTraceDPR;
  ModalRoute<dynamic>? _sidebarTraceRoute;
  final MapViewportState _mapState = MapViewportState();
  late final AgentWorkspaceController _workspace;
  TaskImageController? _taskImages;
  Future<void>? _taskImagesLoad;
  TaskImageDraftController? _imageDraft;
  VideoMaterialController? _videoMaterials;
  Object? _videoMaterialScope;
  SystemPushController? _systemPush;
  Object? _systemPushScope;
  final _systemPushRefresh = ValueNotifier<int>(0);
  final _collectionProjection = ValueNotifier<PersonalCollectionProjection?>(
    null,
  );
  Timer? _collectionProjectionExpiry;
  SavedMapProjectionSource? _savedMapSource;
  Object? _imageDraftObservedScope;
  int _imageDraftEpoch = 0;
  String? _imageDraftCityID;
  Object? _taskImageObservedScope;
  late NowContextQueryController _online;
  NowContextChoice? _viewChoice;
  NowContextChoice? _submittedChoice;
  int _viewSerial = 0;
  int _onlineIntentSerial = 0;
  String? get _queryCityID => _submittedChoice?.queryRoute == 'CITY'
      ? _submittedChoice!.cityID
      : _workspace.task?.cityID ?? widget.city.selectedCity?.id;
  String? get _queryOnlineID => _submittedChoice?.queryRoute == 'ONLINE'
      ? _submittedChoice!.contextID
      : _online.selected?.id;
  late final SavedController _saved;
  late final ActivityPlansController _plans;
  late final ActivityParticipationController _participations;
  late final ConnectionSource _connections;
  late final OrganizationWorkspaceController _organizations;
  late final NowDiscoveryController _discovery;
  late final MapCanvas _mapCanvas;
  late final MapLayersController _layers;
  late final MapMediaController _mapMedia;
  Widget? _publicPlacePanel;
  bool _publicPlacePreview = false, _socialExpanded = false;
  bool _humanConversationExpanded = false;
  bool _publicPlaceStoriesExpanded = false;
  String? _previewObservedSelection;
  String? _publicPlaceMapID;
  late NowSocialEdgeSource _socialSource;
  late RemoteInboxSource _socialInbox;
  late final Listenable _socialChanges;
  final _socialProviderChanges = ValueNotifier<int>(0);
  late final bool Function() _socialCurrent;
  late final String? Function() _socialAccount, _socialWorkspace;
  final _selectedMapAnchor = ValueNotifier<({String? id, Offset? point})>((
    id: null,
    point: null,
  ));
  int _previewSelectionSerial = 0;
  Object? _publicPlacePanelSource;
  bool _publicPlacePanelMapVisible = false;
  late Listenable _activeIntentChanges;
  late final String? Function() _activeIntentWorkspace;
  String? _lastCityID;
  final _credentialTransports = <http.Client, AuthenticatedClient>{};

  http.Client _authenticatedTransport(http.Client? client) {
    if (client == null) return _privateClient;
    if (client is AuthenticatedClient) return client;
    return _credentialTransports.putIfAbsent(
      client,
      () => AuthenticatedClient(
        auth: widget.auth,
        client: client,
        onUnauthorized: _handleRejectedSession,
      ),
    );
  }

  int _authSerial = 0;
  int _workspaceSerial = 0;
  final int _replyContextEpoch = 0;
  (String?, String?) _workspaceIdentity = (null, null);
  String? _seedCheckedAuthorization;
  final _seedEntryChanges = ValueNotifier<int>(0);
  final _placeEntryChanges = ValueNotifier<int>(0);
  late (String?, String?, String?) _seedIdentity;
  int _seedEntryEpoch = 0;
  int _personalSourceSerial = 0;
  Object? _momentEntryBinding;
  int _momentEntryEpoch = 0;

  // A normal bearer renewal changes reading/approval authority, while this
  // logical caller keeps the same person's own text and original selector.
  // Observe every real boundary so A -> B -> A cannot revive an old editor.
  int _observeMomentEntrySource() {
    final binding = (
      widget.auth,
      widget.moments,
      widget.city,
      widget.seedClient,
      widget.seedApiBaseUrl,
      widget.auth.accountID,
      widget.auth.signedIn,
      widget.auth.rejectedSessionOwnerID,
      widget.city.selectedCity?.id,
      _organizations.active?.id,
      _organizations.active?.role.toLowerCase(),
      _personalSourceSerial,
    );
    if (_momentEntryBinding != binding) {
      _momentEntryBinding = binding;
      ++_momentEntryEpoch;
    }
    return _momentEntryEpoch;
  }

  bool _clearingAccount = false;
  BuildContext? _confirmationContext;
  double _composerHeight = 56;
  bool _exploreExpanded = false;
  double _previousKeyboard = 0;
  double? _topControlsHeight;
  double _ornamentHeight = 48;
  final _ornamentTop = ValueNotifier<double?>(null);
  final _mapContentRect = ValueNotifier<Rect?>(null);
  final _mapFocusRevision = ValueNotifier<int>(0);
  double? _resultContentHeight;
  Object? _measuredPresentation;
  bool _cityPickerOpen = false;
  bool _cityPickerVisible = false;
  bool Function()? _cityPickerCurrent;
  final Map<Object, Object> _mapReaderOverlays = {};
  Object get _mapReaderBinding => (
    _layers.epoch,
    _currentSeedIdentity,
    _layers.cityID,
    _layers.bounds,
    _layers.authorID,
    _layers.authorBounds,
  );

  void _mapReaderVisibilityChanged(({Object token, bool visible}) change) {
    if (!mounted) return;
    if (change.visible) {
      _mapReaderOverlays[change.token] = _mapReaderBinding;
    } else {
      // A late old modal cannot release a newer modal's visibility lease.
      _mapReaderOverlays.remove(change.token);
    }
    _layers.updateRefreshVisibility();
  }

  bool _selectingCity = false;
  bool _loadingCityCatalog = false;
  bool Function()? _pendingCityCurrent;
  final Set<String> _recordedImpressions = {};

  void _recordImpression(PublicActivity activity, String source) {
    if (activity.organizationID == null) return;
    if (_recordedImpressions.add('$source:${activity.id}')) {
      unawaited(recordActivityView(activity.id, 'impression', source));
    }
  }

  void _onPublicMomentPhotoChange() {
    final change = publicMomentPhotoChanges.value;
    final origin = (widget.seedApiBaseUrl ?? BirdtieEnvironment.apiBaseUrl)
        .replaceFirst(RegExp(r'/$'), '');
    if (!mounted ||
        change == null ||
        !widget.auth.signedIn ||
        widget.auth.authorizationHeader == null ||
        change.ownerID != widget.auth.accountID ||
        _organizations.active != null ||
        change.origin != origin)
      return;
    _layers.requestMediaRefresh();
  }

  @override
  void initState() {
    super.initState();
    publicMomentPhotoChanges.addListener(_onPublicMomentPhotoChange);
    _nowPerf = NowPerfProbe.fromEnvironment(
      onReport: (report) {
        for (final line in report.toQaLogLines()) {
          debugPrint(line);
        }
      },
    );
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      _nowPerf.beginWindow(
        scene: NowPerfScene.startup,
        refreshRateHz: View.of(context).display.refreshRate,
        duration: const Duration(seconds: 60),
      );
    });
    _privateClient = AuthenticatedClient(
      auth: widget.auth,
      onUnauthorized: _handleRejectedSession,
    );
    WidgetsBinding.instance.addObserver(this);
    if (_sidebarTapTraceEnabled) {
      WidgetsBinding.instance.pointerRouter.addGlobalRoute(
        _observeSidebarTracePointer,
      );
    }
    _online = NowContextQueryController(
      api: NowContextQueryApi(
        authorizationHeader: () => widget.auth.authorizationHeader,
        ownerID: () => widget.auth.accountID,
        organizationWorkspaceID: () => _organizations.active?.id,
        client: _authenticatedTransport(widget.seedClient),
        apiBaseUrl: widget.seedApiBaseUrl,
      ),
    );
    _workspace = AgentWorkspaceController(
      replyRefreshAllowed: () =>
          mounted &&
          ModalRoute.of(context)?.isCurrent == true &&
          WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed,
      source:
          widget.agentTaskSource ??
          RemoteAgentTaskSource(
            client: _privateClient,
            cityID: () => _queryCityID,
            onlineContextID: () => _queryOnlineID,
            onlineApiFactory: () => _online.api,
            authorizationHeader: () => widget.auth.authorizationHeader,
            organizationWorkspaceID: () => _organizations.active?.id,
            publicEvidenceOwnerID: () => widget.auth.accountID,
            publicEvidenceEpoch: () =>
                (_seedEntryEpoch, _workspaceSerial, _replyContextEpoch),
          ),
    );
    _saved = SavedController(
      authorizationHeader: () => widget.auth.authorizationHeader,
      ownerID: () => widget.auth.accountID,
      client: _privateClient,
    );
    _plans = ActivityPlansController(
      authorizationHeader: () => widget.auth.authorizationHeader,
      client: _privateClient,
    );
    _participations = ActivityParticipationController(
      authorizationHeader: () => widget.auth.authorizationHeader,
      client: _privateClient,
    );
    _connections =
        widget.connectionSource ??
        ConnectionSource(
          authorizationHeader: () => widget.auth.authorizationHeader,
          client: _privateClient,
        );
    _organizations = OrganizationWorkspaceController(
      authorizationHeader: () => widget.auth.authorizationHeader,
      client: _privateClient,
    );
    _activeIntentChanges = Listenable.merge([
      widget.auth,
      _organizations,
      _seedEntryChanges,
    ]);
    _activeIntentWorkspace = () => _organizations.active?.id;
    _discovery = NowDiscoveryController(
      authorizationHeader: () => widget.auth.authorizationHeader,
    );
    _mapMedia = MapMediaController(
      client: _authenticatedTransport(widget.seedClient),
      apiBaseUrl: widget.seedApiBaseUrl,
    );
    _layers = MapLayersController(
      api: MapLayersApi(
        client: _authenticatedTransport(widget.seedClient),
        apiBaseUrl: widget.seedApiBaseUrl,
      ),
      authorizationHeader: () => widget.auth.authorizationHeader,
      organizationWorkspaceID: () => _organizations.active?.id,
      refreshAllowed: () =>
          mounted &&
          (ModalRoute.of(context)?.isCurrent == true ||
              (_cityPickerOpen &&
                  _cityPickerVisible &&
                  _cityPickerCurrent?.call() == true) ||
              _mapReaderOverlays.values.any(
                (lease) => lease == _mapReaderBinding,
              )) &&
          WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed,
    );
    _composerDrafts.observeOwner(widget.auth.accountID);
    _socialSource = NowSocialEdgeSource(
      widget.connectionSource ?? _connections,
      onUnauthorized: _handleRejectedSession,
      identityEpoch: () => _seedEntryEpoch,
    );
    _socialInbox = _socialSource.inbox;
    _socialChanges = _seedEntryChanges;
    _socialCurrent = () =>
        mounted &&
        !widget.auth.sessionRestoring &&
        identical(
          _socialSource.connections,
          widget.connectionSource ?? _connections,
        ) &&
        _socialSource.authorization == widget.auth.authorizationHeader;
    _socialAccount = () => widget.auth.accountID;
    _socialWorkspace = () => _organizations.active?.id;
    _mapCanvas = MapCanvas(
      personalCollection: _collectionProjection,
      city: widget.city,
      workspace: _workspace,
      mapState: _mapState,
      discovery: _discovery,
      layers: _layers,
      media: _mapMedia,
      mediaOwnerID: () => widget.auth.accountID,
      mediaAuthorizationHeader: () => widget.auth.authorizationHeader,
      mediaWorkspaceID: () => _organizations.active?.id,
      onInitialViewport: _initialViewport,
      onMapUnavailable: _mapUnavailable,
      onChooseCity: () => unawaited(_chooseCity()),
      ornamentTop: _ornamentTop,
      contentRect: _mapContentRect,
      focusRevision: _mapFocusRevision,
      onEntityPicked: (id) {
        final collection = _collectionProjection.value;
        final savedMap = _savedMapSource;
        if (savedMap != null &&
            identical(savedMap.projection, collection) &&
            savedMap.current) {
          setState(() => savedMap.toggle(id));
          return;
        }
        if (collection != null) {
          if (collection.current &&
              id.startsWith('place:') &&
              collection.entities.any((e) => e.id == id))
            _openPlaceDetails(id.substring(6));
        } else {
          _queuePlacePreview(explicit: true);
        }
      },
      onMapReaderVisibilityChanged: _mapReaderVisibilityChanged,
      onSelectedAnchor: (anchor) {
        if (mounted &&
            anchor.id == _workspace.selectedEntityId &&
            _selectedMapAnchor.value != anchor) {
          _selectedMapAnchor.value = anchor;
        }
      },
      onOrnamentHeightChanged: (height) {
        final measured = height.clamp(48.0, double.infinity);
        if (mounted && (_ornamentHeight - measured).abs() > .5) {
          setState(() => _ornamentHeight = measured);
        }
      },
    );
    _lastCityID = widget.city.selectedCity?.id;
    widget.city.addListener(_onCityChange);
    _organizations.addListener(_refreshWorkspaceTasks);
    widget.auth.addListener(_onAuthChange);
    _seedIdentity = _currentSeedIdentity;
    widget.auth.addListener(_onSeedIdentityChange);
    _organizations.addListener(_onSeedIdentityChange);
    _workspace.addListener(_syncTaskImages);
    _workspace.addListener(_queuePlacePreview);
    _seedEntryChanges.addListener(_syncTaskImages);
    widget.city.addListener(_syncTaskImages);
    _online.addListener(_syncTaskImages);
    _composerDrafts.addListener(_onTypedDraftRestore);
    _composerDrafts.observeWorkspace(_organizations.active?.id);
    _composerDrafts.configurePersistence(
      widget.seedApiBaseUrl ?? _saved.followApiBaseUrl,
      messageOrigin: _saved.followApiBaseUrl,
    );
    _syncTaskImages();
    _lastAuthIdentity = (
      widget.auth.authorizationHeader,
      widget.auth.accountID,
      widget.auth.rejectedSessionOwnerID,
    );
    unawaited(_workspace.loadRecent());
    if (widget.auth.signedIn) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(_checkSeed());
      });
      unawaited(_saved.load());
      unawaited(_plans.load());
      unawaited(_participations.load());
      unawaited(_organizations.load());
    }
  }

  (String?, String?, String?) get _currentSeedIdentity => (
    widget.auth.authorizationHeader,
    widget.auth.accountID,
    _organizations.active?.id,
  );

  Object _taskImageScope() => (
    _workspace.task,
    _workspace.taskEpoch,
    _workspace.queryState == AgentQueryState.loading,
    _seedEntryEpoch,
    _workspaceSerial,
    widget.auth,
    _currentSeedIdentity,
    widget.seedClient,
    widget.seedApiBaseUrl,
    widget.taskImagePendingStore,
    widget.city.selectedCity?.id,
  );

  void _taskImagesChanged() {
    if (!mounted) return;
    if (_creationPublished && !_creationPhotoPublished) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _syncCreationPublicPhoto();
      });
    }
    _queueLocalMediaSync();
    for (final entry in _friendConversations.entries) {
      final retained = _retainedFriendAudio[entry.key],
          controller = entry.value;
      if (retained != null &&
          controller.current &&
          !controller.loading &&
          controller.audioDraft == null &&
          !controller.hasPendingTextOrAudio) {
        _retainedFriendAudio.remove(entry.key);
        if (!controller.restoreAudioDraft(
          retained.draft,
          owner: retained.owner,
          conversationID: entry.key,
        )) {
          _retainedFriendAudio[entry.key] = retained;
        }
      }
    }
    for (final entry in _friendConversations.entries) {
      final retained = _retainedFriendImages[entry.key],
          controller = entry.value;
      if (retained != null &&
          controller.historyAvailable &&
          !controller.loading &&
          !controller.sending &&
          !controller.imageDraft.hasPixels) {
        _retainedFriendImages.remove(entry.key);
        if (controller.restoreImageDraft(
          retained.draft,
          owner: retained.owner,
          conversationID: entry.key,
          recipient: retained.recipient,
        )) {
          retained.draft.dispose();
        } else {
          _retainedFriendImages[entry.key] = retained;
        }
      }
    }
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  void _publishEntryChange(ValueNotifier<int> changes, {bool dispose = false}) {
    void publish() {
      if (dispose || mounted) {
        changes.value++;
        if (dispose) changes.dispose();
      }
    }

    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) => publish());
    } else {
      publish();
    }
  }

  void _syncTaskImages() {
    _syncImageDraft();
    final scope = _taskImageScope();
    if (_taskImageObservedScope == scope) return;
    _taskImageObservedScope = scope;
    _taskImages?.removeListener(_taskImagesChanged);
    _taskImages?.dispose();
    _taskImages = null;
    _taskImagesLoad = null;
    final task = _workspace.task;
    if (_organizations.active != null ||
        widget.auth.sessionRestoring ||
        _workspace.queryState == AgentQueryState.loading ||
        !TaskImageController.eligible(task, widget.auth.accountID) ||
        task?.cityID != widget.city.selectedCity?.id)
      return;
    final controller = TaskImageController(
      task: task!,
      authorizationHeader: () => widget.auth.authorizationHeader,
      ownerID: () => widget.auth.accountID,
      sourceCurrent: () =>
          mounted &&
          _taskImageScope() == scope &&
          _organizations.active == null,
      sourceChanges: Listenable.merge([
        widget.auth,
        _organizations,
        _workspace,
        _seedEntryChanges,
        widget.city,
      ]),
      apiBaseUrl: widget.seedApiBaseUrl ?? RemoteAgentTaskSource.apiBase,
      client: _authenticatedTransport(widget.seedClient),
      pendingStore: widget.taskImagePendingStore,
    );
    _taskImages = controller;
    controller.addListener(_taskImagesChanged);
    _taskImagesLoad = controller.load();
    unawaited(_taskImagesLoad!);
  }

  Object _imageDraftScope() => (
    widget.auth,
    widget.auth.sessionRestoring,
    widget.auth.signedIn,
    widget.auth.accountID,
    widget.auth.rejectedSessionOwnerID,
    widget.city,
    _organizations.active?.id,
    _organizations.active?.role.toLowerCase(),
    _personalSourceSerial,
    _workspaceSerial,
    widget.seedClient,
    widget.seedApiBaseUrl,
    widget.taskImagePicker,
    _imageDraftCityID ?? widget.city.selectedCity?.id,
    _workspace.queryContextType,
    _online.selected?.id,
    _imageDraftEpoch,
  );

  void _syncImageDraft() {
    final scope = _imageDraftScope();
    if (_imageDraftObservedScope == scope && _imageDraft?.current == true)
      return;
    _imageDraftObservedScope = scope;
    _imageDraft?.removeListener(_taskImagesChanged);
    _imageDraft?.dispose();
    _imageDraft = null;
    if (!widget.auth.signedIn ||
        widget.auth.sessionRestoring ||
        _organizations.active != null ||
        _workspace.queryContextType != 'CITY' ||
        widget.city.selectedCity == null)
      return;
    final draft = TaskImageDraftController(
      preserveLocalOnSessionRenewal: true,
      authorizationHeader: () => widget.auth.authorizationHeader,
      ownerID: () => widget.auth.accountID,
      cityID: () => _imageDraftCityID ?? widget.city.selectedCity?.id,
      sourceCurrent: () => mounted && _imageDraftScope() == scope,
      picker: widget.taskImagePicker,
      sourceChanges: Listenable.merge([
        widget.auth,
        _organizations,
        _workspace,
        _seedEntryChanges,
        widget.city,
        _online,
      ]),
    );
    _imageDraftCityID ??= widget.city.selectedCity?.id;
    // Bind the draft to its original city while browsing another destination.
    // It can only be attached to a task in that city, never silently reassigned.
    _imageDraftObservedScope = _imageDraftScope();
    _imageDraft = draft;
    draft.addListener(_taskImagesChanged);
    final retained = _retainedImageDraft;
    if (retained != null &&
        _seedIdentity == _currentSeedIdentity &&
        retained.owner == widget.auth.accountID &&
        retained.city == _imageDraftCityID &&
        draft.restoreLocal(retained)) {
      retained.dispose();
      _retainedImageDraft = null;
    }
  }

  void _retireImageDraft() {
    _imageDraftCityID = null;
    ++_imageDraftEpoch;
    _syncImageDraft();
  }

  Future<void> _openDraftImage({
    MomentImageSource source = MomentImageSource.gallery,
  }) async {
    final draft = _imageDraft;
    if (draft == null || !draft.canSelectSource(source)) return;
    final before = draft.localBytes;
    _composerKey.currentState?.pauseEditing();
    await draft.select(source: source);
    if (mounted &&
        identical(draft, _imageDraft) &&
        draft.current &&
        draft.localBytes != null &&
        !identical(before, draft.localBytes)) {
      ++_localImageSelectionSerial;
      _queueLocalMediaSync();
    }
  }

  VoidCallback? _imageSourceAction(MomentImageSource source) {
    if (_needsLogin) return null;
    if (_composerDrafts.target.human) {
      if (!widget.friendImageEnabled ||
          _friendConversation?.canSelectImageSource(source) != true)
        return null;
      return () => unawaited(
        _composerKey.currentState?.selectFriendImage(source: source) ??
            Future<void>.value(),
      );
    }
    if (_imageDraft?.canSelectSource(source) != true) return null;
    return () => unawaited(_openDraftImage(source: source));
  }

  Future<void> _openTaskImages([TaskImageReceipt? image]) async {
    final controller = _taskImages;
    if (controller == null ||
        !controller.current ||
        !controller.connected ||
        controller.busy)
      return;
    _composerKey.currentState?.pauseEditing();
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) =>
            TaskImagePage(controller: controller, initialImage: image),
      ),
    );
  }

  Widget _taskImageMessage(int index) {
    final controller = _taskImages;
    if (controller == null ||
        !controller.current ||
        !controller.connected ||
        !taskImageDisplayMatches(
          controller.task,
          _workspace.conversation,
          index,
        ))
      return const SizedBox.shrink();
    final rows = controller.images
        .where(
          (image) =>
              image.index == index &&
              image.retainUntil.isAfter(DateTime.now().toUtc()),
        )
        .toList();
    if (rows.isEmpty) return const SizedBox.shrink();
    return Align(
      alignment: Alignment.centerRight,
      child: Wrap(
        spacing: 8,
        children: [
          for (final image in rows)
            ActionChip(
              avatar: const Icon(Icons.image_outlined, size: 18),
              label: const Text('会话图片 · 本人可见'),
              onPressed: controller.busy
                  ? null
                  : () => unawaited(_openTaskImages(image)),
            ),
        ],
      ),
    );
  }

  void _retireSeedEntry() {
    _imageDraftCityID = _retainedImageDraft?.city;
    _workspace.retireReplyRefresh();
    ++_seedEntryEpoch;
    _pendingCityCurrent = null;
    _seedIdentity = _currentSeedIdentity;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) {
          _seedEntryChanges.value++;
        }
      });
    } else {
      _seedEntryChanges.value++;
    }
  }

  void _onSeedIdentityChange() {
    _composerDrafts.configurePersistence(
      widget.seedApiBaseUrl ?? _saved.followApiBaseUrl,
      messageOrigin: _saved.followApiBaseUrl,
    );
    _composerDrafts.observeWorkspace(_organizations.active?.id);
    if (_creation != null && !_creationCurrent) _clearRetiredCreation();
    if (_seedIdentity != _currentSeedIdentity) {
      _composerDrafts.observeOwner(
        widget.auth.accountID,
        rejectedOwner: widget.auth.rejectedSessionOwnerID,
      );
      if (_organizations.active != null && _composerDrafts.target.human) {
        _composerDrafts.select(const NowComposerTarget.agent());
      }
      if (_retainedImageDraft != null &&
          widget.auth.accountID != null &&
          _retainedImageDraft!.owner != widget.auth.accountID) {
        _retainedImageDraft!.dispose();
        _retainedImageDraft = null;
      }
      // Person and workspace changes retire the projection even if an opaque
      // session string happens to stay equal. Returning to A cannot revive it.
      _layers.retire();
      _retireSeedEntry();
    }
  }

  @override
  void didUpdateWidget(MapWorkspace old) {
    super.didUpdateWidget(old);
    if (!identical(old.connectionSource, widget.connectionSource) ||
        old.friendImageEnabled != widget.friendImageEnabled ||
        old.friendImagePicker != widget.friendImagePicker ||
        old.friendImagePendingStore != widget.friendImagePendingStore) {
      for (final controller in _friendConversations.values) {
        controller.removeListener(_taskImagesChanged);
        controller.dispose();
      }
      _friendConversations.clear();
      _clearRetainedFriendImages();
      _composerDrafts.select(const NowComposerTarget.agent());
      _socialRequestScope = null;
      _socialSource = NowSocialEdgeSource(
        widget.connectionSource ?? _connections,
        onUnauthorized: _handleRejectedSession,
        identityEpoch: () => _seedEntryEpoch,
      );
      _socialInbox = _socialSource.inbox;
      _socialProviderChanges.value++;
    }
    if (old.taskImagePicker != widget.taskImagePicker) _syncImageDraft();
    if (old.taskImagePendingStore != widget.taskImagePendingStore)
      _syncTaskImages();
    if (old.city != widget.city) {
      old.city.removeListener(_syncTaskImages);
      old.city.removeListener(_onCityChange);
      widget.city.addListener(_syncTaskImages);
      widget.city.addListener(_onCityChange);
      _taskImageObservedScope = null;
      _syncTaskImages();
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _onCityChange();
      });
    }
    if (old.moments != widget.moments) {
      // Only retire child readers of the replaced personal source. Preserve
      // the independent Now task, map selection and unsent composer draft.
      ++_personalSourceSerial;
      // Source validity changes immediately, including A -> B -> A. Publish
      // the visual update after this build so mounted child readers may retire
      // without notifying their AnimatedBuilders during a different route's build.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _seedEntryChanges.value++;
      });
    }
    final authChanged = old.auth != widget.auth;
    if (authChanged ||
        old.placeDetailClient != widget.placeDetailClient ||
        old.placeDetailApiBaseUrl != widget.placeDetailApiBaseUrl) {
      _publishEntryChange(_placeEntryChanges);
    }
    if (authChanged ||
        old.seedClient != widget.seedClient ||
        old.seedApiBaseUrl != widget.seedApiBaseUrl) {
      ++_viewSerial;
      _viewChoice = null;
      _submittedChoice = null;
      _online.removeListener(_syncTaskImages);
      _online.dispose();
      _mapMedia.replaceTransport(
        client: _authenticatedTransport(widget.seedClient),
        apiBaseUrl: widget.seedApiBaseUrl ?? BirdtieEnvironment.apiBaseUrl,
      );
      _layers.retire(
        api: MapLayersApi(
          client: _authenticatedTransport(widget.seedClient),
          apiBaseUrl: widget.seedApiBaseUrl,
        ),
      );
      _online = NowContextQueryController(
        api: NowContextQueryApi(
          authorizationHeader: () => widget.auth.authorizationHeader,
          ownerID: () => widget.auth.accountID,
          organizationWorkspaceID: () => _organizations.active?.id,
          client: _authenticatedTransport(widget.seedClient),
          apiBaseUrl: widget.seedApiBaseUrl,
        ),
      );
      _online.addListener(_syncTaskImages);
      ++_workspaceSerial;
      _workspace.retireAccountContext();
    }
    if (authChanged) {
      _privateClient.updateAuth(widget.auth);
      for (final client in _credentialTransports.values) {
        client.updateAuth(widget.auth);
      }
      _activeIntentChanges = Listenable.merge([
        widget.auth,
        _organizations,
        _seedEntryChanges,
      ]);
      old.auth.removeListener(_onAuthChange);
      old.auth.removeListener(_onSeedIdentityChange);
      widget.auth.addListener(_onAuthChange);
      widget.auth.addListener(_onSeedIdentityChange);
      ++_authSerial;
      _seedCheckedAuthorization = null;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted) return;
        _lastAuthIdentity = null;
        _onAuthChange();
      });
    }
    if (authChanged ||
        old.seedClient != widget.seedClient ||
        old.seedApiBaseUrl != widget.seedApiBaseUrl) {
      _seedCheckedAuthorization = null;
      _retireSeedEntry();
      // Child builders must receive fresh image controllers before they can
      // subscribe again; the old ONLINE source has already been disposed.
      _syncTaskImages();
    }
    _observeMomentEntrySource();
  }

  Future<void> _openSocialNowRoute() async {
    final auth = widget.auth, client = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch;
    final changes = Listenable.merge([_seedEntryChanges, auth, _organizations]);
    bool current() =>
        mounted &&
        epoch == _seedEntryEpoch &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity;
    if (!current() || identity.$1 == null || identity.$3 != null) return;
    _pauseComposer();
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => SocialNowPage(
            auth: auth,
            client: _authenticatedTransport(client),
            apiBaseUrl: base,
            workspaceChanges: changes,
            organizationWorkspaceID: () => _organizations.active?.id,
            current: current,
          ),
        ),
      ),
    );
  }

  void _retainFriendImage(
    String conversationID,
    FriendConversationController controller,
    String owner,
  ) {
    final draft = controller.retainImageDraft();
    if (draft == null) return;
    _retainedFriendImages.remove(conversationID)?.draft.dispose();
    _retainedFriendImages[conversationID] = (
      owner: owner,
      recipient: controller.conversation.otherAccountId,
      draft: draft,
    );
  }

  void _clearRetainedFriendImages() {
    for (final value in _retainedFriendImages.values) {
      value.draft.dispose();
    }
    _retainedFriendImages.clear();
  }

  Future<void> _handleRejectedSession(String expectedAuthorization) async {
    if (!mounted || widget.auth.authorizationHeader != expectedAuthorization)
      return;
    final retained = _imageDraft?.retainLocal();
    if (retained != null) {
      _retainedImageDraft?.dispose();
      _retainedImageDraft = retained;
    }
    _needsLogin = true;
    final owner = widget.auth.accountID;
    if (owner != null)
      for (final entry in _friendConversations.entries) {
        _retainFriendImage(entry.key, entry.value, owner);
        final draft = entry.value.retainAudioDraft();
        if (draft != null)
          _retainedFriendAudio[entry.key] = (owner: owner, draft: draft);
      }
    final auth = widget.auth;
    try {
      await auth.invalidateRejectedSession(expectedAuthorization);
    } catch (_) {
      /* Access is already retired; a new login replaces the vault. */
    }
    if (mounted && identical(auth, widget.auth)) {
      setState(() {});
      // _onAuthChange owns the recovery flow. A late vault deletion must not
      // reopen login after the user explicitly leaves rejected-session recovery.
    }
  }

  Future<void> _signInHere() {
    final existing = _loginFlow;
    if (existing != null) return existing;
    final auth = widget.auth;
    final future = showBirdtieLoginSheet(
      context,
      auth,
      isCurrent: () =>
          mounted && widget.auth == auth && _organizations.active == null,
      entryChanges: _socialChanges,
    );
    _loginFlow = future;
    return future.whenComplete(() {
      if (identical(_loginFlow, future)) _loginFlow = null;
    });
  }

  void _onAuthChange() {
    _observeMomentEntrySource();
    final token = widget.auth.authorizationHeader;
    final identity = (
      token,
      widget.auth.accountID,
      widget.auth.rejectedSessionOwnerID,
    );
    if (_lastAuthIdentity == identity) {
      // Restoring can finish after token/owner have already been observed.
      // That final notification must also create the local image picker.
      _syncTaskImages();
      return;
    }
    _lastAuthIdentity = identity;
    _mapMedia.retire();
    final rejectedOwner = widget.auth.rejectedSessionOwnerID;
    if (!widget.auth.signedIn && rejectedOwner != null) {
      final retained = _imageDraft?.retainLocal(rejectedOwner: rejectedOwner);
      if (retained != null) {
        _retainedImageDraft?.dispose();
        _retainedImageDraft = retained;
      }
      for (final entry in _friendConversations.entries) {
        _retainFriendImage(entry.key, entry.value, rejectedOwner);
        final draft = entry.value.retainAudioDraft();
        if (draft != null) {
          _retainedFriendAudio[entry.key] = (
            owner: rejectedOwner,
            draft: draft,
          );
        }
      }
      final task = _workspace.task;
      if ((widget.agentTaskSource == null ||
              widget.agentTaskSource is RemoteAgentTaskSource) &&
          task != null &&
          task.principalType == 'PERSON' &&
          task.principalID == rejectedOwner &&
          RegExp(
            r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
          ).hasMatch(task.id)) {
        _rejectedAgentTask = AgentTask(
          id: task.id,
          query: '',
          status: 'RESTORING',
          principalID: rejectedOwner,
          cityID: task.cityID,
          contextType: task.contextType,
          contextID: task.contextID,
        );
      }
    } else if (!widget.auth.signedIn ||
        _rejectedAgentTask?.principalID != widget.auth.accountID) {
      _rejectedAgentTask = null;
    }
    // Capture a rejected owner's detached local pixels before synchronizing
    // the credential-bound controllers. Normal logout cannot use that copy.
    _syncTaskImages();
    if (widget.auth.rejectedSessionOwnerID == null &&
        (!widget.auth.signedIn ||
            _composerDrafts.owner != widget.auth.accountID)) {
      _socialRequestScope = null;
      // Retain a guest's chosen category when their explicit sign-in completes.
      // Logout and a different account still reset the previous owner's view.
      if (!widget.auth.signedIn || _composerDrafts.owner != null) {
        _socialSection = NowSocialSection.messages;
      }
      _socialFollowedContacts = false;
      _socialAllConversations = false;
      _socialMessageUnread = 0;
      _socialNotificationUnread = null;
    }
    _composerDrafts.observeOwner(
      widget.auth.accountID,
      rejectedOwner: widget.auth.rejectedSessionOwnerID,
    );
    if (!widget.auth.signedIn && widget.auth.rejectedSessionOwnerID == null) {
      _retainedImageDraft?.dispose();
      _retainedImageDraft = null;
    }
    if (widget.auth.rejectedSessionOwnerID == null && !widget.auth.signedIn ||
        widget.auth.accountID != null &&
            _retainedFriendAudio.values.any(
              (v) => v.owner != widget.auth.accountID,
            ))
      _retainedFriendAudio.clear();
    if (widget.auth.rejectedSessionOwnerID == null && !widget.auth.signedIn ||
        widget.auth.accountID != null &&
            _retainedFriendImages.values.any(
              (value) => value.owner != widget.auth.accountID,
            )) {
      _clearRetainedFriendImages();
    }
    _needsLogin =
        !widget.auth.signedIn && widget.auth.rejectedSessionOwnerID != null;
    if (_needsLogin) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _needsLogin) unawaited(_signInHere());
      });
    }
    for (final controller in _friendConversations.values) {
      controller.removeListener(_taskImagesChanged);
      controller.dispose();
    }
    _friendConversations.clear();
    _retirePublicPlacePanel();
    _seedCheckedAuthorization = null;
    ++_authSerial;
    _layers.retire();
    _closeConfirmation();
    // This invalidates pending Agent turns synchronously. MapCanvas and its
    // viewport remain the same objects; only the previous account is cleared.
    _clearingAccount = true;
    _clearAccountWorkspace();
    _saved.clear();
    _plans.clear();
    _participations.clear();
    _organizations.clear();
    _clearingAccount = false;
    _discovery.clear();
    final cityID = widget.city.selectedCity?.id;
    final bounds = _mapState.viewportBounds;
    if (cityID != null && bounds != null) {
      unawaited(_discovery.load(cityID, bounds));
      unawaited(_layers.load(cityID, bounds));
    } else if (cityID != null && _mapState.mapError != null) {
      unawaited(_discovery.loadCityActivities(cityID));
    }
    // Later identity listeners retire the old source epoch synchronously.
    // Read guest/owned history only after those listeners have completed.
    final recoveryView = _viewSerial, recoveryEpoch = _authSerial;
    scheduleMicrotask(() async {
      if (mounted && widget.auth.authorizationHeader == token) {
        await _workspace.loadRecent();
        final task = _rejectedAgentTask;
        if (task != null &&
            mounted &&
            recoveryEpoch == _authSerial &&
            recoveryView == _viewSerial &&
            widget.auth.authorizationHeader == token &&
            widget.auth.accountID == task.principalID &&
            widget.auth.signedIn &&
            _organizations.active == null &&
            (widget.agentTaskSource == null ||
                widget.agentTaskSource is RemoteAgentTaskSource)) {
          _rejectedAgentTask = null;
          await _reopenTask(task);
        }
      }
    });
    if (widget.auth.signedIn) {
      unawaited(_checkSeed());
      unawaited(_saved.load());
      unawaited(_plans.load());
      unawaited(_participations.load());
      unawaited(_organizations.load());
      final conversationID = _composerDrafts.target.conversationID;
      final socialSelection = _socialSelectionSerial;
      if (conversationID != null)
        scheduleMicrotask(() {
          if (mounted &&
              widget.auth.authorizationHeader == token &&
              socialSelection == _socialSelectionSerial &&
              _socialExpanded) {
            unawaited(_openConversationByID(conversationID));
          }
        });
    }
  }

  bool _currentAccount(int serial, String? token) =>
      mounted &&
      serial == _authSerial &&
      widget.auth.authorizationHeader == token;

  Future<void> _checkSeed() async {
    final auth = widget.auth, client = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch;
    final token = auth.authorizationHeader;
    if (token == null ||
        token == _seedCheckedAuthorization ||
        _organizations.active != null) {
      return;
    }
    _seedCheckedAuthorization = token;
    final serial = _authSerial;
    bool current() =>
        _currentAccount(serial, token) &&
        epoch == _seedEntryEpoch &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity;
    final controller = AgentSeedController(
      authorizationHeader: () => current() ? auth.authorizationHeader : null,
      accountID: () => current() ? auth.accountID : null,
      organizationWorkspaceID: () => _organizations.active?.id,
      client: _authenticatedTransport(client),
      apiBaseUrl: base,
    );
    try {
      if (!await controller.load() ||
          !mounted ||
          !current() ||
          _organizations.active != null ||
          controller.record?.needsPrompt != true) {
        return;
      }
      // Missing optional profile choices never interrupt ordinary Now usage.
      // The person can open these controls deliberately from their profile.
    } finally {
      controller.dispose();
    }
  }

  void _closeConfirmation() {
    final dialog = _confirmationContext;
    _confirmationContext = null;
    if (dialog == null || !dialog.mounted) return;
    final route = ModalRoute.of(dialog);
    if (route?.isCurrent == true) {
      Navigator.of(dialog).pop();
    } else if (route != null) {
      Navigator.of(dialog).removeRoute(route);
    }
  }

  @override
  void dispose() {
    publicMomentPhotoChanges.removeListener(_onPublicMomentPhotoChange);
    _retireCreationApprovals();
    _disposeCreationPublicPhoto();
    _creationCollection?.dispose();
    _composerDrafts.removeListener(_onTypedDraftRestore);
    unawaited(_composerDrafts.flush());
    _composerDrafts.dispose();
    _collectionProjectionExpiry?.cancel();
    _savedMapSource?.dispose();
    _collectionProjection.dispose();
    _nowPerf.dispose();
    if (_sidebarTapTraceEnabled) {
      WidgetsBinding.instance.pointerRouter.removeGlobalRoute(
        _observeSidebarTracePointer,
      );
      _sidebarTracePointers!.clear();
      _sidebarTraceClock!.stop();
    }
    WidgetsBinding.instance.removeObserver(this);
    _ornamentTop.dispose();
    _mapContentRect.dispose();
    _mapFocusRevision.dispose();
    ++_authSerial;
    _workspace.removeListener(_syncTaskImages);
    _workspace.removeListener(_queuePlacePreview);
    _selectedMapAnchor.dispose();
    _seedEntryChanges.removeListener(_syncTaskImages);
    widget.city.removeListener(_syncTaskImages);
    _online.removeListener(_syncTaskImages);
    _taskImages?.removeListener(_taskImagesChanged);
    _taskImages?.dispose();
    _imageDraft?.removeListener(_taskImagesChanged);
    _imageDraft?.dispose();
    _retainedImageDraft?.dispose();
    _videoMaterials?.removeListener(_taskImagesChanged);
    _videoMaterials?.dispose();
    _localMedia.dispose();
    _systemPush?.dispose();
    _systemPushRefresh.dispose();
    _retainedFriendAudio.clear();
    _clearRetainedFriendImages();
    for (final controller in _friendConversations.values) {
      controller.removeListener(_taskImagesChanged);
      controller.dispose();
    }
    _friendConversations.clear();
    ++_seedEntryEpoch;
    _publishEntryChange(_seedEntryChanges, dispose: true);
    _socialProviderChanges.dispose();
    widget.auth.removeListener(_onSeedIdentityChange);
    _organizations.removeListener(_onSeedIdentityChange);
    _publishEntryChange(_placeEntryChanges, dispose: true);
    widget.city.removeListener(_onCityChange);
    widget.auth.removeListener(_onAuthChange);
    _organizations.removeListener(_refreshWorkspaceTasks);
    _online.dispose();
    _workspace.dispose();
    _saved.dispose();
    _plans.dispose();
    _participations.dispose();
    _connections.dispose();
    _organizations.dispose();
    _discovery.dispose();
    _layers.dispose();
    _mapMedia.dispose();
    _mapState.dispose();
    for (final client in _credentialTransports.values) {
      client.close();
    }
    _credentialTransports.clear();
    _privateClient.close();
    super.dispose();
  }

  void _refreshWorkspaceTasks() {
    _observeMomentEntrySource();
    final current = (
      _organizations.active?.id,
      _organizations.active?.role.toLowerCase(),
    );
    if (current == _workspaceIdentity) return;
    _workspaceIdentity = current;
    ++_workspaceSerial;
    _mapMedia.retire();
    _layers.retire();
    if (_clearingAccount || !widget.auth.signedIn) return;
    _clearAccountWorkspace();
    unawaited(_workspace.loadRecent());
  }

  void _clearAccountWorkspace() {
    ++_viewSerial;
    _pendingCityCurrent = null;
    _viewChoice = null;
    _submittedChoice = null;
    final selection = _workspace.selectedEntityId;
    final publicSelection =
        selection != null &&
        widget.city.places.any(
          (place) =>
              selection == 'place:${place.id}' && place.location.hasPublicPoint,
        );
    _online.retire();
    _workspace.clearAccountContext();
    if (publicSelection) _workspace.selectEntity(selection);
  }

  void _onCityChange() {
    _observeMomentEntrySource();
    final cityID = widget.city.selectedCity?.id;
    if (cityID == _lastCityID) return;
    _retirePublicPlacePanel();
    // A geographic destination does not revoke the current conversation.
    // The map filters that conversation by its original city; identity and
    // projection leases continue to guard its cards and source links.
    _workspace.retireReplyRefresh();
    if (!_selectingCity && !_loadingCityCatalog) {
      ++_viewSerial;
      _pendingCityCurrent = null;
      _workspace.retirePendingQueryForViewChange();
    }
    _lastCityID = cityID;
    _mapMedia.retire();
    _layers.retire();
    _mapState.reset();
    _discovery.clear();
  }

  void _pauseComposer() => _composerKey.currentState?.pauseEditing();

  Rect? _sidebarTraceRect() {
    final surface = _sidebarTraceHeader?.currentContext?.findRenderObject();
    if (surface is! RenderBox || !surface.attached || !surface.hasSize) {
      return null;
    }
    try {
      final rect = surface.localToGlobal(Offset.zero) & surface.size;
      return [
            rect.left,
            rect.top,
            rect.right,
            rect.bottom,
          ].every((value) => value.isFinite)
          ? rect
          : null;
    } catch (_) {
      return null; // Unavailable layout is unknown, not guessed.
    }
  }

  void _traceSidebar(
    String event, {
    PointerEvent? pointer,
    bool? reportedOpened,
  }) {
    if (!_sidebarTapTraceEnabled || !mounted) return;
    final scaffold = _scaffold.currentState;
    final rect = _sidebarTraceRect(), dpr = _sidebarTraceDPR;
    List<double>? coordinates(double scale) => rect == null
        ? null
        : [
            rect.left * scale,
            rect.top * scale,
            rect.right * scale,
            rect.bottom * scale,
          ];
    final point = pointer?.position;
    final hasPoint = point != null && point.dx.isFinite && point.dy.isFinite;
    debugPrint(
      'BIRDTIE_SIDEBAR_TAP_TRACE ${jsonEncode({
        'event': event,
        'sequence': ++_sidebarTraceSequence,
        'elapsedMicros': _sidebarTraceClock!.elapsedMicroseconds,
        'mounted': mounted,
        'viewId': _sidebarTraceViewID,
        'dpr': dpr,
        'headerAttached': rect != null,
        'logicalHeaderRect': coordinates(1),
        'physicalHeaderRect': dpr == null ? null : coordinates(dpr),
        'scaffoldPresent': scaffold != null,
        'scaffoldHash': scaffold == null ? null : identityHashCode(scaffold),
        'isDrawerOpen': scaffold?.isDrawerOpen,
        'routeCurrent': _sidebarTraceRoute?.isCurrent,
        'lifecycle': WidgetsBinding.instance.lifecycleState?.name,
        'schedulerPhase': SchedulerBinding.instance.schedulerPhase.name,
        if (pointer != null) 'pointer': pointer.pointer,
        if (hasPoint) 'logicalPoint': [point.dx, point.dy],
        if (hasPoint && dpr != null) 'physicalPoint': [point.dx * dpr, point.dy * dpr],
        if (reportedOpened != null) 'reportedOpened': reportedOpened,
      })}',
    );
  }

  void _observeSidebarTracePointer(PointerEvent event) {
    if (!_sidebarTapTraceEnabled || !mounted) return;
    // Observe delivery before the existing view/header hit filters. Unknown
    // geometry or another view must stay unknown; no pointer coordinates or
    // content enter this record. This never dispatches or retries an action.
    final eventType = event is PointerDownEvent
        ? 'down'
        : event is PointerUpEvent
        ? 'up'
        : event is PointerCancelEvent
        ? 'cancel'
        : null;
    if (eventType != null) {
      final rect = _sidebarTraceRect();
      final comparable =
          event.viewId == _sidebarTraceViewID &&
          rect != null &&
          event.position.dx.isFinite &&
          event.position.dy.isFinite;
      debugPrint(
        'BIRDTIE_SIDEBAR_TAP_TRACE ${jsonEncode({'event': 'global_delivery', 'eventType': eventType, 'count': ++_sidebarTraceDeliveryCount, 'sequence': ++_sidebarTraceSequence, 'elapsedMicros': _sidebarTraceClock!.elapsedMicroseconds, 'viewId': event.viewId, 'expectedViewId': _sidebarTraceViewID, 'headerAttached': rect != null, 'insideHeader': comparable ? rect.contains(event.position) : null})}',
      );
    }
    if (event.viewId != _sidebarTraceViewID) return;
    final pointers = _sidebarTracePointers!;
    if (event is PointerDownEvent) {
      if (_sidebarTraceRect()?.contains(event.position) != true) return;
      pointers.add(event.pointer);
      _traceSidebar('global_pointer_down', pointer: event);
    } else if ((event is PointerUpEvent || event is PointerCancelEvent) &&
        pointers.remove(event.pointer)) {
      if (event is PointerUpEvent) {
        _traceSidebar('global_pointer_up', pointer: event);
      } else if (event is PointerCancelEvent) {
        _traceSidebar('global_pointer_cancel', pointer: event);
      }
    }
  }

  Widget _observeSidebarTraceSurface(BuildContext context, Widget child) {
    if (!_sidebarTapTraceEnabled) return child;
    final view = View.of(context);
    _sidebarTraceViewID = view.viewId;
    _sidebarTraceDPR = view.devicePixelRatio;
    _sidebarTraceRoute = ModalRoute.of(context);
    return Listener(
      behavior: HitTestBehavior.deferToChild,
      onPointerDown: (event) =>
          _traceSidebar('header_pointer_down', pointer: event),
      onPointerUp: (event) =>
          _traceSidebar('header_pointer_up', pointer: event),
      onPointerCancel: (event) =>
          _traceSidebar('header_pointer_cancel', pointer: event),
      child: child,
    );
  }

  Future<void> _openNowTools() async {
    _pauseComposer();
    final auth = widget.auth, client = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch, viewSerial = _viewSerial;
    final cityID = widget.city.selectedCity?.id,
        onlineID = _online.selected?.id;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        viewSerial == _viewSerial &&
        cityID == widget.city.selectedCity?.id &&
        onlineID == _online.selected?.id;
    final selected = await showModalBottomSheet<Future<void> Function()>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      builder: (sheetContext) => ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(sheetContext).height * .7,
        ),
        child: NotificationDestinationBoundary(
          identityChanges: Listenable.merge([
            _activeIntentChanges,
            widget.city,
            _online,
          ]),
          current: current,
          builder: (_) => SafeArea(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                key: const Key('now-tools-menu'),
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          '更多工具',
                          style: Theme.of(sheetContext).textTheme.titleLarge,
                        ),
                      ),
                      IconButton(
                        tooltip: '关闭更多工具',
                        constraints: const BoxConstraints(
                          minWidth: 48,
                          minHeight: 48,
                        ),
                        onPressed: () => Navigator.pop(sheetContext),
                        icon: const Icon(Icons.close),
                      ),
                    ],
                  ),
                  Expanded(
                    child: ListView(
                      children: [
                        for (final tool in [
                          (
                            Icons.language,
                            '线上社交发现',
                            '线上社交发现',
                            _openOnlineOpportunities,
                          ),
                          (
                            Icons.travel_explore,
                            '查看范围',
                            '选择查看范围',
                            _openViewChoice,
                          ),
                          (
                            Icons.layers_outlined,
                            '地图图层',
                            '地图图层',
                            _openMapLayers,
                          ),
                          (
                            Icons.flag_outlined,
                            '我的社交意图',
                            '我的社交意图',
                            _openActiveIntentCard,
                          ),
                          (
                            Icons.edit_note_outlined,
                            '私人草稿',
                            '打开私人草稿',
                            () => _openIntentDraft(),
                          ),
                        ])
                          Tooltip(
                            message: tool.$3,
                            child: ListTile(
                              minTileHeight: 48,
                              contentPadding: const EdgeInsets.symmetric(
                                horizontal: 8,
                              ),
                              leading: Icon(
                                tool.$1,
                                color: Theme.of(
                                  sheetContext,
                                ).colorScheme.onSurface,
                              ),
                              title: Text(tool.$2),
                              onTap: () {
                                if (current()) {
                                  Navigator.pop(sheetContext, tool.$4);
                                }
                              },
                            ),
                          ),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
    if (current() && selected != null) await selected();
  }

  Future<void> _openViewChoice() async {
    if (!widget.auth.signedIn) await _signInHere();
    if (!mounted || !widget.auth.signedIn || _organizations.active != null)
      return;
    _pauseComposer();
    final auth = widget.auth, rawClient = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        rawClient == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch;
    final changes = Listenable.merge([auth, _organizations, _seedEntryChanges]);
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (routeContext) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => NowContextSelectionPage(
            auth: auth,
            client: _authenticatedTransport(rawClient),
            apiBaseUrl: base,
            workspaceChanges: changes,
            organizationWorkspaceID: () => _organizations.active?.id,
            onSelect: (choice) {
              if (!current()) return;
              setState(() {
                ++_viewSerial;
                _viewChoice = choice;
              });
              Navigator.pop(routeContext);
            },
          ),
        ),
      ),
    );
  }

  Future<void> _openIntentDraft({AgentTask? task}) async {
    _pauseComposer();
    if (task != null && !identical(task, _workspace.task)) return;
    final auth = widget.auth, client = widget.seedClient;
    final base = widget.seedApiBaseUrl, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        _organizations.active == null;
    if (_organizations.active != null) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请切换到个人身份后管理私人意图草稿。')));
      return;
    }
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (routeContext) => NotificationDestinationBoundary(
          identityChanges: _activeIntentChanges,
          current: current,
          builder: (_) => SocialIntentDraftPage(
            authorizationHeader: () => auth.authorizationHeader,
            authorizationChanges: auth,
            ownerID: () => auth.accountID,
            workspaceID: _activeIntentWorkspace,
            workspaceChanges: _activeIntentChanges,
            current: current,
            client: _authenticatedTransport(client),
            apiBaseUrl: base,
            agentTaskId: task?.id,
            suggestedTitle: task?.query,
            suggestedCategory: task?.filters['category'],
            contextNote: task == null ? '仅自己可见' : '仅自己可见 · 请检查查询中建议的内容',
            onClose: () => Navigator.pop(routeContext),
            onManageIntent: (id) {
              if (!current()) return;
              unawaited(
                Navigator.of(routeContext).push<void>(
                  MaterialPageRoute(
                    builder: (_) => NotificationDestinationBoundary(
                      identityChanges: _activeIntentChanges,
                      current: current,
                      builder: (_) => ActiveSocialIntentPage(
                        auth: auth,
                        client: _authenticatedTransport(client),
                        apiBaseUrl: base,
                        workspaceChanges: _activeIntentChanges,
                        organizationWorkspaceID: _activeIntentWorkspace,
                        initialIntentID: id,
                      ),
                    ),
                  ),
                ),
              );
            },
          ),
        ),
      ),
    );
  }

  void _selectPublicCity(String id) {
    final pending = _workspace.pendingScopeQuery;
    final resume = pending != null && _pendingCityCurrent?.call() == true;
    if (!resume && widget.city.selectedCity?.id == id) return;
    if (!resume) {
      ++_viewSerial;
      _pendingCityCurrent = null;
      _workspace.retirePendingQueryForViewChange();
    }
    _viewChoice = null;
    _submittedChoice = null;
    _selectingCity = true;
    try {
      widget.city.selectCity(id);
    } finally {
      _selectingCity = false;
    }
    // Consume before awaiting, so repeated taps and retired ABA cannot replay.
    if (resume && widget.city.selectedCity?.id == id) {
      _pendingCityCurrent = null;
      unawaited(
        _workspace.resumeCity(id, widget.city.activities, widget.city.places),
      );
    }
  }

  Future<void> _chooseCity() async {
    if (_cityPickerOpen) return;
    _pauseComposer();
    final binding = _mapReaderBinding;
    final identity = _currentSeedIdentity, epoch = _seedEntryEpoch;
    final viewSerial = _viewSerial, auth = widget.auth;
    final client = widget.seedClient, base = widget.seedApiBaseUrl;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        viewSerial == _viewSerial &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        binding == _mapReaderBinding;
    Future<void> reload() async {
      if (!current() || _loadingCityCatalog) return;
      _loadingCityCatalog = true;
      try {
        await widget.city.loadCities();
      } finally {
        _loadingCityCatalog = false;
      }
    }

    String? selected;
    _cityPickerOpen = true;
    _cityPickerVisible = false;
    _cityPickerCurrent = current;
    _layers.updateRefreshVisibility();
    if (widget.city.cities.isEmpty) unawaited(reload());
    try {
      if (!mounted || !current()) return;
      selected = await showModalBottomSheet<String>(
        context: context,
        showDragHandle: true,
        isScrollControlled: true,
        useSafeArea: true,
        builder: (sheetContext) => CurrentMapReaderVisibility(
          current: current,
          onChanged: (visible) {
            if (!mounted || !_cityPickerOpen || _cityPickerCurrent != current) {
              return;
            }
            _cityPickerVisible = visible && current();
            _layers.updateRefreshVisibility();
          },
          child: _NowCityPicker(
            city: widget.city,
            identityChanges: _activeIntentChanges,
            current: current,
            reloadCities: reload,
            onClose: (value) => Navigator.pop(sheetContext, value),
          ),
        ),
      );
    } finally {
      _cityPickerOpen = false;
      _cityPickerVisible = false;
      _cityPickerCurrent = null;
      _layers.updateRefreshVisibility();
    }
    if (!mounted || !current()) return;
    if (selected != null) {
      _selectPublicCity(selected);
    } else if (_layers.publicView == null &&
        _layers.error != null &&
        !_layers.loading &&
        _layers.cityID == widget.city.selectedCity?.id &&
        _layers.bounds != null) {
      // The geographic picker keeps the same map scope. Resume a skipped
      // local read after its original lease expired; never extend that lease.
      unawaited(_layers.load(_layers.cityID!, _layers.bounds!));
    }
  }

  void _initialViewport(MapBounds bounds) {
    _mapState.initializeViewport(bounds);
    final cityID = widget.city.selectedCity?.id;
    if (cityID != null) {
      unawaited(_discovery.load(cityID, bounds));
      unawaited(_layers.load(cityID, bounds));
    }
  }

  void _mapUnavailable(String message) {
    if (_mapState.mapError == message) return;
    _mapState.mapUnavailable(message);
    final cityID = widget.city.selectedCity?.id;
    if (cityID != null) unawaited(_discovery.loadCityActivities(cityID));
  }

  Future<void> _reopenTask(AgentTask task) async {
    if (_publicPlacePanel != null) setState(_retirePublicPlacePanel);
    final serial = ++_viewSerial;
    _viewChoice = null;
    _submittedChoice = null;
    final epoch = _authSerial,
        workspaceEpoch = _workspaceSerial,
        auth = widget.auth,
        token = widget.auth.authorizationHeader,
        owner = widget.auth.accountID,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    bool current() =>
        mounted &&
        serial == _viewSerial &&
        epoch == _authSerial &&
        workspaceEpoch == _workspaceSerial &&
        auth == widget.auth &&
        token == widget.auth.authorizationHeader &&
        owner == widget.auth.accountID &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl;
    if (task.contextType == 'ONLINE') {
      try {
        final response = await _online.api.restore(task.id);
        if (!current()) return;
        _online.restoreContext(response.context);
        if (!current()) return;
      } catch (error) {
        if (mounted && current()) {
          final message = error is AgentRequestFailure
              ? [error.userMessage, ?error.recoveryMessage].join('\n')
              : '此线上任务的情境或来源已不可用。';
          ScaffoldMessenger.of(
            context,
          ).showSnackBar(SnackBar(content: Text(message)));
        }
        return;
      }
    } else {
      _online.select(null);
      if (task.cityID != null) {
        widget.city.selectCity(task.cityID!, rememberExploration: false);
      }
    }
    await _workspace.reopen(task, widget.city.activities, widget.city.places);
  }

  Future<void> _openOnlineIntent(String id) async {
    final token = widget.auth.authorizationHeader,
        epoch = _authSerial,
        workspaceEpoch = _workspaceSerial,
        viewSerial = _viewSerial,
        requestSerial = ++_onlineIntentSerial,
        owner = widget.auth.accountID,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final auth = widget.auth;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        token == widget.auth.authorizationHeader &&
        owner == widget.auth.accountID &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        viewSerial == _viewSerial &&
        requestSerial == _onlineIntentSerial &&
        epoch == _authSerial &&
        workspaceEpoch == _workspaceSerial &&
        _organizations.active == null;
    try {
      final response = await _online.api.intent(id);
      if (!mounted || !current()) return;
      await Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => NotificationDestinationBoundary(
            identityChanges: Listenable.merge([
              auth,
              _organizations,
              _seedEntryChanges,
            ]),
            current: current,
            builder: (_) => _NowOnlineDetail(response: response),
          ),
        ),
      );
    } catch (error) {
      if (mounted && current()) {
        final message = error is AgentRequestFailure
            ? [error.userMessage, ?error.recoveryMessage].join('\n')
            : '此意图已不可用或权限发生变化，请重新查询。';
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(SnackBar(content: Text(message)));
      }
    }
  }

  bool _humanSubmissionCurrent(
    Object identity,
    int revision,
    String caption,
    FriendConversationController controller,
    int epoch,
  ) =>
      mounted &&
      _composerDrafts.identity == identity &&
      _humanDraftRevision == revision &&
      _composerDrafts.value.text == caption &&
      identical(_friendConversation, controller) &&
      controller.imageEpoch == epoch &&
      controller.current &&
      _organizations.active == null;

  Future<bool> _imageJournalAction(
    PendingFriendImage pending,
    bool retry,
  ) async {
    final friend = _friendConversation;
    if (friend == null || !_composerDrafts.target.human) return false;
    final identity = _composerDrafts.identity,
        draftVersion = _composerDrafts.contentVersion,
        revision = _humanDraftRevision,
        caption = _composerDrafts.value.text,
        epoch = friend.imageEpoch;
    bool current() =>
        _humanSubmissionCurrent(identity, revision, caption, friend, epoch);
    final matching = friend.canRetryImage(pending, caption);
    final sent = retry
        ? await friend.retryImage(pending, caption, draftCurrent: current)
        : await friend.recoverImage(
            pending,
            caption: caption,
            draftCurrent: current,
          );
    if (sent && matching && current()) {
      _composerDrafts.clearSent(
        identity,
        expectedText: caption,
        expectedVersion: draftVersion,
        preserveMaterials: true,
      );
      ++_humanDraftRevision;
    }
    if (mounted) setState(() {});
    return sent;
  }

  bool _localMaterialConfirmationOpen = false;
  Future<void> _confirmTextMaterialScope(String query) async {
    if (_localMaterialConfirmationOpen || !mounted) return;
    final identity = _composerDrafts.identity,
        version = _composerDrafts.contentVersion,
        source = _personalSourceSerial;
    final originalText = _composerDrafts.value.text;
    bool current() =>
        mounted &&
        !_composerDrafts.suspended &&
        _composerDrafts.identity == identity &&
        _composerDrafts.contentVersion == version &&
        _composerDrafts.value.text == originalText &&
        source == _personalSourceSerial;
    _localMaterialConfirmationOpen = true;
    final changes = Listenable.merge([
      _composerDrafts,
      widget.auth,
      _organizations,
      _seedEntryChanges,
    ]);
    final route = DialogRoute<bool>(
      context: context,
      builder: (context) => AnimatedBuilder(
        animation: changes,
        builder: (context, _) => AlertDialog(
          title: const Text('确认本次发送内容'),
          content: current()
              ? Text('这次发送当前文字及另行确认的材料。@ 引用和附件记录继续保留在本机，不会自动带入。\n\n$query')
              : const Text('草稿或发送目标已变化，请关闭后重新检查。'),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('返回检查'),
            ),
            FilledButton(
              onPressed: current()
                  ? () {
                      if (current()) Navigator.pop(context, true);
                    }
                  : null,
              child: const Text('继续发送'),
            ),
          ],
        ),
      ),
    );
    try {
      final yes = await Navigator.of(context).push(route);
      await route.completed;
      if (yes == true && current())
        _submit(query, localMaterialApproval: version);
    } finally {
      _localMaterialConfirmationOpen = false;
    }
  }

  void _submit(String query, {String? localMaterialApproval}) {
    if (_composerDrafts.suspended) {
      unawaited(_signInHere());
      return;
    }
    if (_creationCurrent) {
      _updateCreation(query);
      return;
    }
    final localDraft = _composerDrafts.envelope;
    if (query.isNotEmpty &&
        localDraft != null &&
        (localDraft.references.isNotEmpty ||
            localDraft.attachments.isNotEmpty) &&
        localMaterialApproval != localDraft.contentVersion) {
      unawaited(_confirmTextMaterialScope(query));
      return;
    }
    final friend = _friendConversation;
    if (_composerDrafts.target.human) {
      if (friend == null ||
          !friend.current ||
          _organizations.active != null ||
          ModalRoute.of(context)?.isCurrent != true ||
          WidgetsBinding.instance.lifecycleState != AppLifecycleState.resumed)
        return;
      final value = _composerDrafts.value;
      if (value.composing.isValid && !value.composing.isCollapsed) return;
      final image = friend.imageDraft.localBytes != null;
      // Image captions retain exact whitespace; ordinary text keeps its existing
      // trim behavior. All IME/arrow submissions use this same typed dispatch.
      if (image ? value.text != query : value.text.trim() != query) return;
      final identity = _composerDrafts.identity,
          draftVersion = _composerDrafts.contentVersion,
          revision = _humanDraftRevision,
          originalText = value.text,
          epoch = friend.imageEpoch;
      bool current() => _humanSubmissionCurrent(
        identity,
        revision,
        originalText,
        friend,
        epoch,
      );
      final operation = image
          ? friend.sendImage(query, draftCurrent: current)
          : query.isEmpty
          ? friend.sendAudio()
          : friend.sendText(query);
      unawaited(
        operation.then((sent) {
          if (!mounted) return;
          if (sent && current()) {
            _composerDrafts.clearSent(
              identity,
              expectedText: originalText,
              expectedVersion: draftVersion,
              preserveMaterials: true,
            );
            ++_humanDraftRevision;
          }
          setState(() {});
        }),
      );
      return;
    }
    _collectionProjectionExpiry?.cancel();
    _collectionProjection.value = null;
    _composerDrafts.update(
      TextEditingValue(
        text: query,
        selection: TextSelection.collapsed(offset: query.length),
      ),
    );
    _socialSelectionSerial++;
    setState(() => _socialExpanded = false);
    unawaited(_submitInSelectedView(query));
  }

  bool _preserveUnfinishedTaskImage([String? query]) {
    final images = _taskImages;
    if (images == null ||
        !images.current ||
        (images.localBytes == null &&
            images.preview == null &&
            !images.uncertain &&
            images.pendingOperations.isEmpty))
      return false;
    if (query != null && _composerKey.currentState?.draftText.isEmpty == true) {
      _composerKey.currentState?.fillDraft(query, requestFocus: false);
    }
    ScaffoldMessenger.of(context).showSnackBar(
      const SnackBar(content: Text('上一条消息的图片尚未保存或核实。请先保存、移除本机图片或核实原操作，再继续提问。')),
    );
    unawaited(_openTaskImages());
    return true;
  }

  Future<void> _submitInSelectedView(String query) async {
    final draftIdentity = _composerDrafts.identity;
    final draftVersion = _composerDrafts.contentVersion;
    if (_publicPlacePanel != null) setState(_retirePublicPlacePanel);
    if (_preserveUnfinishedTaskImage(query)) return;
    if (_imageDraft?.localBytes != null &&
        _imageDraftCityID != widget.city.selectedCity?.id) {
      _composerKey.currentState?.fillDraft(query, requestFocus: false);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('图片草稿仍属于原城市。请返回原城市发送，或移除图片后继续。')),
      );
      return;
    }
    _pendingCityCurrent = null;
    final choice = _viewChoice,
        serial = ++_viewSerial,
        identity = _currentSeedIdentity,
        epoch = _seedEntryEpoch,
        token = widget.auth.authorizationHeader,
        owner = widget.auth.accountID;
    bool current() =>
        mounted &&
        serial == _viewSerial &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        token == widget.auth.authorizationHeader &&
        owner == widget.auth.accountID;
    if (choice != null) {
      if (choice.queryRoute == 'UNAVAILABLE' ||
          token == null ||
          owner == null ||
          _organizations.active != null) {
        if (mounted && current()) {
          _composerKey.currentState?.fillDraft(query, requestFocus: false);
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('该查看范围当前不支持查询；请选择城市或本人线上情境。')),
          );
        }
        return;
      }
      final api = NowContextSelectionAPI(
        client: _authenticatedTransport(widget.seedClient),
        apiBaseUrl: widget.seedApiBaseUrl,
      );
      try {
        final options = await api.options(token, owner);
        if (!current()) return;
        final candidates = options.items.where((o) => o.same(choice.option));
        if (candidates.length != 1) throw const FormatException('来源已变化');
        final refreshed = await api.resolve(
          token,
          owner,
          options,
          candidates.single,
        );
        if (!current()) return;
        if (refreshed.queryRoute == 'CITY') {
          final cityID = refreshed.cityID;
          if (!widget.city.cities.any((city) => city.id == cityID)) {
            throw const FormatException('城市目录已变化');
          }
          // The receipt was revalidated for this submit. Keep the map on that
          // canonical city without retiring the query's existing authority.
          // Choosing a view in the picker alone never changes the map.
          _selectingCity = true;
          try {
            widget.city.selectCity(cityID, rememberExploration: false);
          } finally {
            _selectingCity = false;
          }
          if (!current()) return;
          if (widget.city.selectedCity?.id != cityID) {
            throw const FormatException('城市范围无法应用');
          }
        }
        final task = _workspace.task;
        final sameScope =
            task != null &&
            (refreshed.queryRoute == 'CITY'
                ? task.contextType == 'CITY' && task.cityID == refreshed.cityID
                : task.contextType == 'ONLINE' &&
                      task.contextID == refreshed.contextID);
        _viewChoice = refreshed;
        _submittedChoice = refreshed;
        _workspace.queryContextType = refreshed.queryRoute;
        if (refreshed.queryRoute == 'ONLINE') {
          _online.restoreContext(
            NowOnlineContext(id: refreshed.contextID, label: refreshed.label),
          );
        } else {
          _online.select(null);
        }
        if (!sameScope) _workspace.newTask(preserveMapSelection: true);
      } catch (_) {
        if (mounted && current()) {
          _composerKey.currentState?.fillDraft(query, requestFocus: false);
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('查看范围或会话已变化，请重新选择。原任务、结果和输入已保留。')),
          );
        }
        return;
      } finally {
        api.dispose();
      }
    }
    if (!current()) return;
    final previousScope = _workspace.task;
    if (_workspace.queryContextType == 'CITY' &&
        previousScope != null &&
        previousScope.cityID != widget.city.selectedCity?.id) {
      // Keep the old conversation when browsing another city. Only an explicit
      // new submission starts a task in that destination, never a picker tap.
      _workspace.newTask(preserveMapSelection: true);
    }
    if (_queryCityID == null && _queryOnlineID == null) {
      _workspace.requireCity(query);
      _pendingCityCurrent = current;
      return;
    }
    final material = _imageDraft?.snapshot();
    final previousTask = _workspace.result == null ? null : _workspace.task;
    final prefix = previousTask?.messages.toList() ?? <AgentMessage>[];
    final sending = _workspace.submit(
      query,
      widget.city.activities,
      widget.city.places,
      cityID: _queryCityID,
    );
    final sendingEpoch = _workspace.taskEpoch;
    await sending;
    if (!current() || _workspace.taskEpoch != sendingEpoch) return;
    if (_workspace.requestError != null) {
      if (_composerKey.currentState?.draftText.isEmpty == true) {
        _composerKey.currentState?.fillDraft(query, requestFocus: false);
      }
      if (_workspace.requestFailure?.statusCode == 401 && token != null) {
        await _handleRejectedSession(token);
      }
      return;
    }
    _composerDrafts.clearSent(
      draftIdentity,
      expectedText: query,
      expectedVersion: draftVersion,
      preserveMaterials: true,
      taskID: _workspace.task?.id,
    );
    if (mounted) setState(() {});
    if (material == null || !material.isCurrent) return;
    final task = _workspace.task;
    final index = prefix.length;
    bool exactTurn() =>
        task != null &&
        (previousTask == null || task.id == previousTask.id) &&
        task.messages.length > index &&
        task.messages[index].role == 'user' &&
        task.messages[index].text == query.trim() &&
        List.generate(prefix.length, (i) => i).every(
          (i) =>
              task.messages[i].role == prefix[i].role &&
              task.messages[i].text == prefix[i].text,
        );
    if (!exactTurn()) return;
    _syncTaskImages();
    final controller = _taskImages;
    final loading = _taskImagesLoad;
    if (loading != null) await loading;
    if (!current() ||
        _workspace.taskEpoch != sendingEpoch ||
        _taskImages != controller ||
        !material.isCurrent ||
        controller == null)
      return;
    await controller.sendReviewedDraft(
      material,
      messageIndex: index,
      submittedText: query.trim(),
    );
  }

  Future<void> _openOnlineOpportunities() async {
    final identity = _currentSeedIdentity,
        epoch = _seedEntryEpoch,
        auth = widget.auth;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: _activeIntentChanges,
          current: current,
          builder: (_) => OnlineSocialOpportunityPage(
            auth: auth,
            client: _authenticatedTransport(widget.seedClient),
            apiBaseUrl: widget.seedApiBaseUrl,
            workspaceChanges: _activeIntentChanges,
            organizationWorkspaceID: _activeIntentWorkspace,
          ),
        ),
      ),
    );
  }

  void _searchThisArea({String query = '搜索此区域'}) {
    if (_preserveUnfinishedTaskImage()) return;
    final bounds = _mapState.searchAreaBounds ?? _mapState.viewportBounds;
    if (bounds == null) return;
    _mapState.beginSearch();
    final cityID = widget.city.selectedCity?.id;
    if (cityID == null) return;
    var epoch = _layers.epoch;
    final identity = _currentSeedIdentity;
    final author = _layers.authorID;
    final kinds = author == null ? Set.of(_layers.visible) : const {'MOMENT'};
    bool scopeCurrent() =>
        mounted &&
        epoch == _layers.epoch &&
        identity == _currentSeedIdentity &&
        cityID == widget.city.selectedCity?.id &&
        author == _layers.authorID &&
        (author != null ||
            (_layers.visible.length == kinds.length &&
                _layers.visible.every(kinds.contains)));
    unawaited(
      _workspace.executeMapArea(
        bounds: bounds,
        cityID: cityID,
        authorID: author,
        kinds: kinds,
        reader: () async {
          if (!scopeCurrent())
            throw const AgentRequestFailure('地图范围已变化，请重新搜索。');
          if (author == null) {
            final loading = _layers.load(cityID, bounds);
            // First city initialization synchronously retires the old layer
            // epoch. Bind this command to that read before awaiting network.
            epoch = _layers.epoch;
            await loading;
          } else {
            await _layers.loadAuthor(cityID, bounds, author);
          }
          final public = author == null
              ? _layers.publicView
              : _layers.authorView;
          final private = author == null ? _layers.privateView : null;
          if (!scopeCurrent() ||
              public == null ||
              (author == null ? _layers.bounds : _layers.authorBounds) !=
                  bounds) {
            throw AgentRequestFailure(
              author == null
                  ? _layers.error ?? '地图内容暂不可用，请重新搜索。'
                  : _layers.authorError ?? '作者范围暂不可用，请重新搜索。',
            );
          }
          return AgentAreaRead(
            publicView: public,
            privateView: private,
            unavailableKinds: author == null
                ? _layers.unavailableKinds
                : const {},
            current: () =>
                scopeCurrent() &&
                (author == null ? _layers.bounds : _layers.authorBounds) ==
                    bounds &&
                _layers.retainsSnapshot(public) &&
                (private == null
                    ? (author == null ? _layers.privateView : null) == null
                    : _layers.retainsSnapshot(private)),
            remaining: () =>
                public.validUntil.difference(DateTime.now().toUtc()),
          );
        },
      ),
    );
  }

  AgentResult? get _mapProjectionResult =>
      _workspace.queryContextType == 'ONLINE'
      ? _workspace.mapBackgroundResult
      : _workspace.presentedResult;

  List<MapEntity> _visibleEntities() {
    if (_layers.authorID != null) {
      return _layers.authorCityID == widget.city.selectedCity?.id
          ? _layers.items.map((i) => i.entity).toList(growable: false)
          : const [];
    }
    final area = _workspace.areaOperation;
    if (area?.current == true && area!.cityID == widget.city.selectedCity?.id) {
      return area.items.map((item) => item.entity).toList(growable: false);
    }
    final online = _workspace.queryContextType == 'ONLINE';
    final result = online
        ? _workspace.mapBackgroundResult
        : _workspace.presentedResult;
    final cityID = online
        ? _workspace.mapBackgroundCityID
        : _workspace.task?.cityID;
    if ((result?.projectionItems != null ||
            result?.onlinePlaceSearch != null) &&
        cityID == widget.city.selectedCity?.id) {
      return result!.entities;
    }
    if (_layers.cityID != widget.city.selectedCity?.id) return const [];
    return _layers.items.map((i) => i.entity).toList(growable: false);
  }

  Future<void> _openMapLayers() async {
    final binding = _mapReaderBinding;
    final epoch = _layers.epoch,
        auth = widget.auth,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final identity = _currentSeedIdentity;
    bool current() =>
        mounted &&
        epoch == _layers.epoch &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        binding == _mapReaderBinding;
    final readerToken = Object();
    try {
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        builder: (sheetContext) => CurrentMapReaderVisibility(
          current: current,
          onChanged: (visible) => _mapReaderVisibilityChanged((
            token: readerToken,
            visible: visible && current(),
          )),
          child: SizedBox(
            height: MediaQuery.sizeOf(sheetContext).height * .8,
            child: MapLayerControls(
              controller: _layers,
              current: current,
              onRefresh:
                  _mapState.viewportBounds == null ||
                      widget.city.selectedCity == null
                  ? null
                  : () {
                      final city = widget.city.selectedCity?.id,
                          bounds = _mapState.viewportBounds;
                      if (current() && city != null && bounds != null) {
                        unawaited(_layers.load(city, bounds));
                      }
                    },
              onOpen: (item) {
                if (!current() || _layers.find(item.mapID) == null) return;
                Navigator.pop(sheetContext);
                _workspace.selectEntity(item.mapID);
                _openEntityDetails(item.entity);
              },
            ),
          ),
        ),
      );
    } finally {
      _mapReaderVisibilityChanged((token: readerToken, visible: false));
    }
  }

  void _openEntityDetails(MapEntity entity) {
    final result = _mapProjectionResult;
    if (result != null && _workspace.canUseExternalPlaces(result)) {
      final candidates = result.onlinePlaceSearch!.places.where(
        (place) => place.mapID == entity.id,
      );
      if (candidates.length == 1 &&
          _visibleEntities().any((visible) => visible.id == entity.id)) {
        unawaited(
          showExternalPlaceDetails(
            context,
            search: result.onlinePlaceSearch!,
            place: candidates.single,
            current: () => _workspace.canUseExternalPlaces(result),
            changes: Listenable.merge([_workspace, widget.auth, widget.city]),
          ),
        );
        return;
      }
    }
    if (entity.id.startsWith('external-place:')) return;
    final matching = result?.projectionItems?.where(
      (i) => i.entity.mapID == entity.id,
    );
    if (_visibleEntities().any((visible) => visible.id == entity.id) &&
        matching != null &&
        matching.length == 1) {
      unawaited(
        _agentResultRouter(mapBackground: true).open(context, matching.single),
      );
      return;
    }
    final item = _layers.find(entity.id);
    if (item != null) {
      unawaited(_openTypedMapDetail(item));
      return;
    }
  }

  Widget _selectionCard(MapEntity entity) {
    final result = _mapProjectionResult;
    final search = result?.onlinePlaceSearch;
    final place = search?.places
        .where((place) => place.mapID == entity.id)
        .firstOrNull;
    if (place != null &&
        result != null &&
        _workspace.canUseExternalPlaces(result)) {
      return ExternalPlaceCandidateCard(
        place: place,
        stale: search!.stale,
        onOpen: () => _openEntityDetails(entity),
      );
    }
    return EntityPeekCard(
      entity: entity,
      onOpen: () => _openEntityDetails(entity),
    );
  }

  AgentEntityResultRouter _agentResultRouter({
    bool mapBackground = false,
    AgentResult? reply,
  }) {
    final identity = _currentSeedIdentity,
        epoch = _seedEntryEpoch,
        result =
            reply ?? (mapBackground ? _mapProjectionResult : _workspace.result);
    return AgentEntityResultRouter(
      auth: widget.auth,
      workspaceChanges: Listenable.merge([
        widget.auth,
        _organizations,
        _seedEntryChanges,
        _workspace,
      ]),
      workspaceID: () => _organizations.active?.id,
      apiBaseUrl: widget.seedApiBaseUrl,
      client: _authenticatedTransport(widget.seedClient),
      current: (item) =>
          mounted &&
          identity == _currentSeedIdentity &&
          epoch == _seedEntryEpoch &&
          (result?.replyProjectionCurrent ?? false) &&
          (reply != null
              ? _workspace.retainsReply(reply)
              : identical(
                  result,
                  mapBackground ? _mapProjectionResult : _workspace.result,
                )) &&
          (result?.projectionItems?.any((i) => identical(i, item)) ?? false),
    );
  }

  void _focusReplyPlace(AgentResult source, AgentResultItem item) {
    if (!_agentResultRouter(reply: source).current(item) ||
        item.anchor?.precision != 'point')
      return;
    final cityID = source.task?.cityID ?? _workspace.task?.cityID;
    if (cityID != null &&
        cityID != widget.city.selectedCity?.id &&
        widget.city.cities.any((city) => city.id == cityID)) {
      _selectPublicCity(cityID);
    }
    if (!_agentResultRouter(reply: source).current(item)) return;
    _workspace.showReplyOnMap(source, item.entity.mapID);
    if (_workspace.selectedEntityId == item.entity.mapID) {
      _mapFocusRevision.value++;
    }
  }

  Future<void> _navigateReplyPlace(
    AgentResult source,
    AgentResultItem item,
  ) async {
    final router = _agentResultRouter(reply: source), anchor = item.anchor;
    if (!router.current(item) ||
        item.entity.type != 'place' ||
        anchor?.precision != 'point')
      return;
    final token = widget.auth.authorizationHeader,
        owner = widget.auth.accountID;
    final organization = _organizations.active?.id;
    bool current() =>
        router.current(item) &&
        token == widget.auth.authorizationHeader &&
        owner == widget.auth.accountID &&
        organization == _organizations.active?.id;
    final client = widget.seedClient ?? http.Client();
    Uri? destination;
    try {
      await runEntityAction(
        context,
        ref: EntityActionRef('place', item.entity.id),
        kind: EntityActionKind.navigate,
        authorizationHeader: () => current() ? token : null,
        accountID: () => current() ? owner : null,
        workspaceID: () => _organizations.active?.id,
        identityChanges: router.workspaceChanges,
        client: _authenticatedTransport(client),
        apiBaseUrl: widget.seedApiBaseUrl,
        domainCurrent: current,
        prepareReview: (_, _) async {
          final base = widget.seedApiBaseUrl ?? BirdtieEnvironment.apiBaseUrl;
          final response = await client
              .get(
                Uri.parse(
                  '$base/v1/places/${Uri.encodeComponent(item.entity.id)}',
                ),
                headers: token == null ? const {} : {'Authorization': token},
              )
              .timeout(const Duration(seconds: 12));
          if (!current() || response.statusCode != 200) return false;
          final place = PublicPlace.fromJson(
            (jsonDecode(utf8.decode(response.bodyBytes))
                    as Map<String, dynamic>)['data']
                as Map<String, dynamic>,
          );
          final point = place.location;
          if (place.id != item.entity.id ||
              !point.hasPublicPoint ||
              point.latitude != anchor!.latitude ||
              point.longitude != anchor.longitude)
            return false;
          destination = Uri.https('www.google.com', '/maps/dir/', {
            'api': '1',
            'destination': '${point.latitude},${point.longitude}',
          });
          return true;
        },
        reviewDetails: (_) => '目的地：${item.title}。使用当前公开地点打开外部地图。',
        handler: (_) async {
          if (!current() || destination == null) return;
          if (!await launchUrl(
                destination!,
                mode: LaunchMode.externalApplication,
              ) &&
              mounted) {
            ScaffoldMessenger.of(
              context,
            ).showSnackBar(const SnackBar(content: Text('无法打开导航，请检查设备设置。')));
          }
        },
      );
    } catch (_) {
      if (mounted && current())
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('导航地点暂不可用，请更新来源后重试。')));
    } finally {
      if (widget.seedClient == null) client.close();
    }
  }

  void _measureResultHeight(double height) {
    if (mounted &&
        height.isFinite &&
        height >= 0 &&
        (_resultContentHeight == null ||
            (_resultContentHeight! - height).abs() > .5)) {
      setState(() => _resultContentHeight = height);
    }
  }

  Future<void> _openActiveIntentCard() async {
    final auth = widget.auth,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final identity = _currentSeedIdentity, epoch = _seedEntryEpoch;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (sheetContext) => SizedBox(
        height: MediaQuery.sizeOf(sheetContext).height * .8,
        child: NotificationDestinationBoundary(
          identityChanges: _activeIntentChanges,
          current: current,
          builder: (_) => Scaffold(
            body: SafeArea(
              child: SingleChildScrollView(
                padding: const EdgeInsets.all(16),
                child: ActiveSocialIntentCard(
                  auth: auth,
                  client: _authenticatedTransport(client),
                  apiBaseUrl: base,
                  workspaceChanges: _activeIntentChanges,
                  organizationWorkspaceID: _activeIntentWorkspace,
                  onClose: () => Navigator.of(sheetContext).pop(),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _openTypedMapDetail(TypedMapItem item) async {
    if (item.kind == 'PLACE' || item.kind == 'MOMENT') {
      _openPublicPlacePanel(item);
      return;
    }
    final auth = widget.auth,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final identity = _currentSeedIdentity, epoch = _layers.epoch;
    final changes = Listenable.merge([
      auth,
      _organizations,
      _seedEntryChanges,
      _layers,
    ]);
    bool current() =>
        mounted &&
        auth == widget.auth &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl &&
        identity == _currentSeedIdentity &&
        epoch == _layers.epoch &&
        _layers.find(item.mapID)?.sourceVersion == item.sourceVersion;
    if (!current()) return;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => ChatEntityDetail(
            type: item.detailType.toLowerCase(),
            id: item.detailID,
            auth: auth,
            workspaceChanges: changes,
            workspaceID: () => _organizations.active?.id,
            apiBaseUrl: base,
            client: _authenticatedTransport(client),
          ),
        ),
      ),
    );
  }

  void _queuePlacePreview({bool explicit = false}) {
    final selected = _workspace.selectedEntityId;
    if (_friendConversation?.audioActive == true) {
      _previewObservedSelection = selected;
      ++_previewSelectionSerial;
      return;
    }
    if (!explicit && _previewObservedSelection == selected) return;
    final serial = ++_previewSelectionSerial;
    _previewObservedSelection = selected;
    if (_publicPlacePanel != null) {
      if (!explicit || !_publicPlacePreview) return;
      _retirePublicPlacePanel(leaveAuthor: false);
    }
    final item = selected == null ? null : _layers.find(selected);
    if (item == null || !{'PLACE', 'MOMENT'}.contains(item.kind)) return;
    final layerEpoch = _layers.epoch, identityEpoch = _seedEntryEpoch;
    final author = _layers.authorID;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted &&
          _friendConversation?.audioActive != true &&
          layerEpoch == _layers.epoch &&
          identityEpoch == _seedEntryEpoch &&
          author == _layers.authorID &&
          serial == _previewSelectionSerial &&
          _publicPlacePanel == null &&
          selected == _workspace.selectedEntityId &&
          _layers.find(selected!)?.sourceVersion == item.sourceVersion) {
        _openPublicPlacePanel(item);
      }
    });
  }

  void _toggleSocial() => _toggleSocialView(NowSocialSection.messages);
  void _toggleNotifications() =>
      _toggleSocialView(NowSocialSection.notifications);

  void _toggleSocialView(NowSocialSection section) {
    final selectorWasVisible =
        _socialExpanded &&
        !_workspace.inputFocused &&
        MediaQuery.viewInsetsOf(context).bottom == 0 &&
        _publicPlacePanel == null;
    _pauseComposer();
    if (_composerDrafts.target.human && section == NowSocialSection.messages) {
      if (!_leaveSocialConversation()) return;
      setState(() {
        _socialExpanded = true;
        _socialSection = section;
      });
      return;
    }
    final sameEntry = section == NowSocialSection.notifications
        ? _socialSection == NowSocialSection.notifications
        : _socialSection != NowSocialSection.notifications;
    if (selectorWasVisible && sameEntry) {
      _closeSocial();
      return;
    }
    if (!_leaveSocialConversation()) return;
    // A queued map preview must not replace this newer explicit social intent.
    ++_previewSelectionSerial;
    setState(() {
      _retirePublicPlacePanel();
      _socialSection = section;
      _socialRequestScope = null;
      _socialAllConversations = false;
      _socialExpanded = true;
    });
  }

  bool _leaveSocialConversation() {
    if (_friendConversation?.audioActive == true) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先停止或取消录音，录音和草稿会保留。')));
      return false;
    }
    _socialSelectionSerial++;
    if (_composerDrafts.target.human) _exitFriendConversation();
    return true;
  }

  void _closeSocial() {
    if (!_leaveSocialConversation()) return;
    setState(() {
      _socialExpanded = false;
      _socialRequestScope = null;
    });
  }

  void _showSocialRequests(String scope) {
    if (!_socialCurrent() ||
        !widget.auth.signedIn ||
        !_leaveSocialConversation())
      return;
    setState(() => _socialRequestScope = scope);
  }

  Future<void> _openContactAction(String action) async {
    if (!_socialCurrent() ||
        !widget.auth.signedIn ||
        _organizations.active != null ||
        !_leaveSocialConversation())
      return;
    _pauseComposer();
    final changes = Listenable.merge([_socialChanges, _socialProviderChanges]);
    final source = _socialSource.connections;
    final child = switch (action) {
      'scan' => ContactScanPage(
        auth: widget.auth,
        client: _authenticatedTransport(source.followClient),
        apiBaseUrl: source.followApiBaseUrl,
        identityChanges: changes,
        workspaceID: () => _organizations.active?.id,
        onSignIn: _signInHere,
        onUnauthorized: _handleRejectedSession,
      ),
      'own' => MyContactCodePage(
        auth: widget.auth,
        client: _authenticatedTransport(source.followClient),
        apiBaseUrl: source.followApiBaseUrl,
        identityChanges: changes,
        workspaceID: () => _organizations.active?.id,
        onSignIn: _signInHere,
      ),
      _ => ContactSearchPage(
        auth: widget.auth,
        client: _authenticatedTransport(source.followClient),
        apiBaseUrl: source.followApiBaseUrl,
        identityChanges: changes,
        workspaceID: () => _organizations.active?.id,
        onSignIn: _signInHere,
        onUnauthorized: _handleRejectedSession,
      ),
    };
    await Navigator.of(
      context,
    ).push<void>(MaterialPageRoute(builder: (_) => child));
  }

  void _syncSystemPush() {
    final auth = widget.auth,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final owner = auth.accountID, token = auth.authorizationHeader;
    final epoch = _seedEntryEpoch, source = _personalSourceSerial;
    final scope = (
      auth,
      owner,
      token,
      client,
      base,
      epoch,
      source,
      _organizations.active?.id,
    );
    if (_systemPushScope == scope) return;
    _systemPushScope = scope;
    _systemPush?.dispose();
    _systemPush = null;
    if (!auth.signedIn || _organizations.active != null) return;
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        auth.accountID == owner &&
        auth.authorizationHeader == token &&
        identical(client, widget.seedClient) &&
        base == widget.seedApiBaseUrl &&
        epoch == _seedEntryEpoch &&
        source == _personalSourceSerial &&
        _organizations.active == null;
    final changes = Listenable.merge([auth, _organizations, _seedEntryChanges]);
    _systemPush = SystemPushController(
      authorizationHeader: () => current() ? token : null,
      ownerID: () => current() ? owner : null,
      sourceCurrent: current,
      identityChanges: changes,
      client: _authenticatedTransport(client),
      apiBaseUrl: base ?? RemoteAgentTaskSource.apiBase,
      onForeground: () {
        if (current()) _systemPushRefresh.value++;
      },
      onOpen: (item, event) async {
        if (!current() || _systemPush?.view?.matches(event) != true) return;
        _pauseComposer();
        final view = _systemPush!.view;
        bool destinationCurrent() =>
            current() &&
            _systemPush?.current == true &&
            _systemPush?.view?.bindingID == view?.bindingID &&
            _systemPush?.view?.version == view?.version &&
            _systemPush?.view?.matches(event) == true &&
            view?.matches(event) == true;
        await Navigator.of(context).push<void>(
          MaterialPageRoute(
            builder: (_) => NotificationDestinationBoundary(
              identityChanges: Listenable.merge([changes, _systemPush!]),
              current: destinationCurrent,
              builder: (_) => Scaffold(
                appBar: AppBar(title: const Text('通知')),
                body: InboxPanel(
                  auth: auth,
                  source: _socialInbox,
                  initialItem: item,
                  workspaceChanges: changes,
                  organizationWorkspaceID: () => _organizations.active?.id,
                  onOpenActivity: (id) {
                    if (destinationCurrent())
                      unawaited(_openActivityByID(id, source: 'inbox'));
                  },
                  onOpenConversation: (id) {
                    if (destinationCurrent())
                      unawaited(_openConversationByID(id));
                  },
                  onOrganizationAccepted: _organizations.load,
                ),
              ),
            ),
          ),
        );
      },
    );
    unawaited(_systemPush!.initialize());
  }

  Future<void> _openAgentVisual(String query) async {
    _syncLocalMedia();
    final image = _imageDraft;
    final pixels = image?.localBytes;
    if (!mounted ||
        image == null ||
        pixels == null ||
        !image.current ||
        image.busy ||
        _composerDrafts.target.human ||
        _organizations.active != null ||
        _composerDrafts.value.text.trim() != query ||
        !widget.auth.signedIn)
      return;
    _pauseComposer();
    final auth = widget.auth, city = widget.city;
    final owner = auth.accountID;
    final revision = image.localRevision;
    final selectedDigest = sha256.convert(pixels).toString();
    final selectedSerial = _localImageSelectionSerial;
    final source = _observeMomentEntrySource();
    final client = widget.seedClient, base = widget.seedApiBaseUrl;
    final identity = _composerDrafts.identity;
    final draftVersion = _composerDrafts.contentVersion;
    if (draftVersion == null) return;
    final cityID = image.cityID();
    if (cityID == null || cityID != city.selectedCity?.id) return;
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        identical(city, widget.city) &&
        auth.signedIn &&
        auth.authorizationHeader != null &&
        auth.accountID == owner &&
        _organizations.active == null &&
        identical(client, widget.seedClient) &&
        base == widget.seedApiBaseUrl &&
        _observeMomentEntrySource() == source &&
        _localImageSelectionSerial == selectedSerial &&
        _imageDraft?.current == true &&
        _imageDraft?.busy == false &&
        _imageDraft?.localBytes != null &&
        sha256.convert(_imageDraft!.localBytes!).toString() == selectedDigest &&
        _composerDrafts.identity == identity &&
        _composerDrafts.contentVersion == draftVersion &&
        _composerDrafts.value.text.trim() == query &&
        !_composerDrafts.target.human &&
        city.selectedCity?.id == cityID;
    final changes = Listenable.merge([
      auth,
      city,
      _organizations,
      _seedEntryChanges,
      _workspace,
      _online,
      image,
      _composerDrafts,
    ]);
    final controller = AgentVisualController(
      authorizationHeader: () => current() ? auth.authorizationHeader : null,
      ownerID: () => current() ? owner : null,
      sourceContextID: cityID,
      sourceContextType: 'CITY',
      sourceCurrent: current,
      draftCurrent: current,
      identityChanges: changes,
      bytes: pixels,
      userText: query,
      clientDraftRevision: revision,
      masked: image.locallyMasked,
      client: _authenticatedTransport(client),
      apiBaseUrl: base ?? RemoteAgentTaskSource.apiBase,
    );
    final navigator = Navigator.of(context);
    late final MaterialPageRoute<void> visualRoute;
    visualRoute = MaterialPageRoute<void>(
      builder: (_) => NotificationDestinationBoundary(
        identityChanges: changes,
        current: current,
        builder: (_) => AgentVisualPage(
          controller: controller,
          onSaveLocalDraft: (LocalVisualDraft draft) async {
            if (!current() ||
                draft.sourceSelection.ownerID != owner ||
                draft.sourceSelection.clientDraftID !=
                    controller.clientDraftID ||
                draft.sourceSelection.clientDraftRevision != revision ||
                draft.sourceSelection.selectedImageSHA256 != selectedDigest)
              return false;
            final saved = await _composerDrafts.saveEditedText(
              draft.text,
              expectedIdentity: identity,
              expectedVersion: draftVersion,
            );
            if (saved &&
                mounted &&
                navigator.mounted &&
                visualRoute.isCurrent &&
                identical(visualRoute.navigator, navigator)) {
              navigator.pop();
            }
            return saved;
          },
        ),
      ),
    );
    unawaited(visualRoute.completed.then((_) => controller.dispose()));
    try {
      await navigator.push(visualRoute);
      await visualRoute.completed;
    } finally {
      controller.dispose();
    }
  }

  Future<void> _openVideoMaterials() async {
    if (_composerDrafts.target.human) return;
    _pauseComposer();
    if (!widget.auth.signedIn) await _signInHere();
    if (!mounted || !widget.auth.signedIn || _composerDrafts.target.human)
      return;
    if (_organizations.active != null) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请切换到个人身份后添加视频。')));
      return;
    }
    final auth = widget.auth, city = widget.city;
    final token = auth.authorizationHeader, owner = auth.accountID;
    final epoch = _seedEntryEpoch, source = _personalSourceSerial;
    final client = widget.seedClient, base = widget.seedApiBaseUrl;
    final draftIdentity = _composerDrafts.identity;
    final taskEpoch = _workspace.taskIdentityEpoch;
    final selectedCity = city.selectedCity?.id;
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        identical(city, widget.city) &&
        auth.signedIn &&
        auth.authorizationHeader == token &&
        auth.accountID == owner &&
        epoch == _seedEntryEpoch &&
        source == _personalSourceSerial &&
        draftIdentity == _composerDrafts.identity &&
        taskEpoch == _workspace.taskIdentityEpoch &&
        selectedCity == city.selectedCity?.id &&
        identical(client, widget.seedClient) &&
        base == widget.seedApiBaseUrl &&
        _organizations.active == null;
    final changes = Listenable.merge([
      auth,
      city,
      _organizations,
      _seedEntryChanges,
      _composerDrafts,
      _workspace,
    ]);
    final scope = (
      auth,
      token,
      owner,
      epoch,
      source,
      client,
      base,
      draftIdentity,
      taskEpoch,
      selectedCity,
    );
    if (_videoMaterialScope != scope || _videoMaterials?.current != true) {
      _videoMaterials?.removeListener(_taskImagesChanged);
      _videoMaterials?.dispose();
      _videoMaterialScope = scope;
      _videoMaterials = VideoMaterialController(
        authorizationHeader: () => current() ? token : null,
        ownerID: () => current() ? owner : null,
        sourceCurrent: current,
        identityChanges: changes,
        client: _authenticatedTransport(client),
        apiBaseUrl: base,
      );
      _videoMaterials!.addListener(_taskImagesChanged);
    }
    // Picking a material stays in Now and never implies an upload or a post.
    await _videoMaterials!.select();
    if (mounted && current()) {
      _queueLocalMediaSync();
      setState(() {});
    }
  }

  Future<void> _choosePlaceToSave() async {
    _pauseComposer();
    if (!widget.auth.signedIn) await _signInHere();
    if (!mounted || !widget.auth.signedIn) return;
    final result = _workspace.result;
    final router = _agentResultRouter();
    final choices =
        result?.projectionItems
            ?.where(
              (item) =>
                  item.bookmarkKind == 'place' &&
                  item.detail != null &&
                  router.current(item) &&
                  !_saved.contains('place', item.detail!.id),
            )
            .toList() ??
        const <AgentResultItem>[];
    if (choices.isEmpty) return;
    final chosen = await showModalBottomSheet<AgentResultItem>(
      context: context,
      showDragHandle: true,
      useSafeArea: true,
      builder: (sheetContext) => AnimatedBuilder(
        animation: Listenable.merge([
          widget.auth,
          _organizations,
          _seedEntryChanges,
          _workspace,
        ]),
        builder: (_, __) => choices.every(router.current)
            ? ListView(
                shrinkWrap: true,
                children: [
                  const ListTile(title: Text('选择要收藏的地点')),
                  for (final item in choices)
                    ListTile(
                      title: Text(item.title),
                      onTap: () {
                        if (router.current(item) &&
                            ModalRoute.of(sheetContext)?.isCurrent == true)
                          Navigator.pop(sheetContext, item);
                      },
                    ),
                ],
              )
            : const Padding(
                padding: EdgeInsets.all(24),
                child: Text('身份或内容已变化，请重新选择地点。'),
              ),
      ),
    );
    if (!mounted || chosen == null || !router.current(chosen)) return;
    await router.save(context, chosen, _saved);
  }

  Future<void> _openContactProfile(String personID) async {
    if (!_socialCurrent() ||
        !widget.auth.signedIn ||
        _organizations.active != null ||
        !_leaveSocialConversation())
      return;
    _pauseComposer();
    final source = _socialSource.connections;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => PublicPersonPage(
          accountID: personID,
          auth: widget.auth,
          authorizationHeader: () => widget.auth.authorizationHeader,
          client: _authenticatedTransport(source.followClient),
          apiBaseUrl: source.followApiBaseUrl,
          identityChanges: Listenable.merge([
            widget.auth,
            _socialChanges,
            _socialProviderChanges,
          ]),
          workspaceID: () => _organizations.active?.id,
        ),
      ),
    );
  }

  Widget _socialRequests(String scope) => SocialRequestsPanel(
    key: ValueKey('social-requests-$scope'),
    auth: widget.auth,
    source: _socialSource,
    scope: scope,
    identityChanges: _socialChanges,
    current: () => _socialCurrent() && _organizations.active == null,
  );

  Widget _socialNotifications() => _socialRequestScope == 'friend'
      ? Column(
          children: [
            Align(
              alignment: Alignment.centerLeft,
              child: IconButton(
                onPressed: () => setState(() => _socialRequestScope = null),
                icon: const Icon(Icons.arrow_back, size: 18),
                tooltip: '返回通知列表',
              ),
            ),
            Expanded(child: _socialRequests('friend')),
          ],
        )
      : Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: () => _showSocialRequests('friend'),
                icon: const Icon(Icons.person_add_outlined, size: 18),
                label: const Text('好友申请'),
              ),
            ),
            Flexible(
              fit: FlexFit.loose,
              child: InboxPanel(
                auth: widget.auth,
                source: _socialInbox,
                embedded: true,
                notificationsOnly: true,
                refreshChanges: _systemPushRefresh,
                previewLimit: 3,
                onUnreadChanged: (value) {
                  if (mounted && _socialNotificationUnread != value) {
                    setState(() => _socialNotificationUnread = value);
                  }
                },
                onUnreadInvalidated: () {
                  if (mounted && _socialNotificationUnread != null) {
                    setState(() => _socialNotificationUnread = null);
                  }
                },
                workspaceChanges: _organizations,
                organizationWorkspaceID: () => _organizations.active?.id,
                onOrganizationAccepted: _organizations.load,
                onUsePersonal: () => _organizations.select(null),
                onSignIn: () => unawaited(_signInHere()),
                onOpenActivity: (id) =>
                    unawaited(_openActivityByID(id, source: 'inbox')),
                onOpenConversation: (id) =>
                    unawaited(_openConversationByID(id)),
              ),
            ),
          ],
        );

  void _retirePublicPlacePanel({bool leaveAuthor = true}) {
    _publicPlacePanel = null;
    _publicPlacePreview = false;
    _publicPlaceStoriesExpanded = false;
    _publicPlaceMapID = null;
    _publicPlacePanelSource = null;
    _publicPlacePanelMapVisible = false;
    if (leaveAuthor) _layers.leaveAuthor();
  }

  void _openPublicPlacePanel(
    TypedMapItem item, {
    bool openPlacePreview = false,
  }) {
    if (_friendConversation?.audioActive == true) return;
    if (!{'PLACE', 'MOMENT'}.contains(item.kind) ||
        item.placeID.isEmpty ||
        (item.kind == 'MOMENT' &&
            (item.detailType != 'MOMENT' || item.detailID != item.id))) {
      return;
    }
    final auth = widget.auth,
        client = widget.seedClient,
        base = widget.seedApiBaseUrl;
    final identity = _currentSeedIdentity,
        epoch = _layers.epoch,
        city = widget.city.selectedCity?.id;
    final originatingAuthor = _layers.authorID;
    // Only this reader's explicit navigation may change its author scope. Keep
    // the original source as the authority even when an author list is empty.
    var expectedAuthor = originatingAuthor;
    var originRetired = false;
    final changes = Listenable.merge([
      auth,
      widget.city,
      _organizations,
      _seedEntryChanges,
      _layers,
    ]);
    final source = Object();
    bool originCurrent() {
      if (originRetired) return false;
      final original = originatingAuthor == null
          ? _layers.findBase(item.mapID)
          : _layers.find(item.mapID);
      final valid =
          mounted &&
          auth == widget.auth &&
          client == widget.seedClient &&
          base == widget.seedApiBaseUrl &&
          identity == _currentSeedIdentity &&
          city == widget.city.selectedCity?.id &&
          epoch == _layers.epoch &&
          expectedAuthor == _layers.authorID &&
          original?.sourceVersion == item.sourceVersion;
      // A -> B -> A, including original-source withdrawal, cannot revive an
      // old callback after its mounted reader has observed the invalidation.
      if (!valid) originRetired = true;
      return valid;
    }

    bool current() =>
        originCurrent() && identical(_publicPlacePanelSource, source);
    if (!originCurrent() || city == null) return;
    if (_composerDrafts.target.human) _exitFriendConversation();
    _publicPlacePanelSource = source;
    _socialSelectionSerial++;
    _socialExpanded = false;
    _publicPlaceMapID = item.mapID;
    final priorSelection = _workspace.selectedEntityId;
    final taskEpoch = _workspace.taskEpoch;
    // Author browsing is a read of taskless Now's existing city range. It
    // cannot replace a live Agent reply or borrow a different workspace.
    bool authorCurrent() =>
        current() &&
        taskEpoch == _workspace.taskEpoch &&
        _workspace.task == null &&
        _organizations.active == null;
    String? authorPriorSelection;
    void leaveAuthor() {
      if (!identical(_publicPlacePanelSource, source)) return;
      final selected = authorPriorSelection;
      final active = _layers.authorID != null;
      expectedAuthor = null;
      _layers.leaveAuthor();
      if (mounted) setState(() => _publicPlacePanelMapVisible = false);
      if (active && authorCurrent()) {
        final restore = selected != null && _layers.findBase(selected) != null
            ? selected
            : null;
        _workspace.selectEntity(restore, preserveViewport: true);
      }
    }

    Future<bool> readAuthor(String author) async {
      if (!authorCurrent() ||
          (originatingAuthor != null && author != originatingAuthor)) {
        return false;
      }
      final bounds = _layers.authorBounds ?? _layers.bounds;
      if (bounds == null) return false;
      // Recheck the original public source before using its author navigation.
      await _layers.load(city, _layers.bounds ?? bounds);
      if (!authorCurrent() || _layers.error != null) return false;
      if (_layers.authorID == null) {
        authorPriorSelection = _workspace.selectedEntityId;
        _workspace.selectEntity(null, preserveViewport: true);
      }
      // loadAuthor publishes its selected scope synchronously before its GET.
      // Bind that one requested transition before observers validate it.
      expectedAuthor = author;
      final success = await _layers.loadAuthor(city, bounds, author);
      return success && authorCurrent();
    }

    if (originatingAuthor == null) _layers.leaveAuthor();
    _publicPlacePanelMapVisible = false;
    _pauseComposer();
    // Opening public content is a read of the current browsing range. Highlight
    // its canonical entity without flying the existing camera or losing the list.
    _workspace.selectEntity(item.mapID, preserveViewport: true);
    void closePanel() {
      if (!mounted || !identical(_publicPlacePanelSource, source)) return;
      if (originatingAuthor == null) leaveAuthor();
      if (auth == widget.auth &&
          identity == _currentSeedIdentity &&
          city == widget.city.selectedCity?.id &&
          taskEpoch == _workspace.taskEpoch) {
        // A withdrawn entity cannot be restored from the saved selection.
        if (epoch == _layers.epoch) {
          final restore =
              priorSelection != null &&
                  _visibleEntities().any((e) => e.id == priorSelection)
              ? priorSelection
              : null;
          _workspace.selectEntity(restore, preserveViewport: true);
        } else if (_workspace.selectedEntityId == item.mapID) {
          // Retiring this source clears only this panel's temporary choice.
          // A newer selection or task keeps its own current state.
          _workspace.selectEntity(null, preserveViewport: true);
        }
      }
      setState(
        () => _retirePublicPlacePanel(leaveAuthor: originatingAuthor == null),
      );
    }

    Widget reader({String? initialMomentID, bool directMoment = false}) =>
        NowPublicPlacePanel(
          initialMomentID: initialMomentID,
          initialObjectDetail: directMoment,
          key: UniqueKey(),
          item: item,
          auth: auth,
          identityChanges: changes,
          workspaceID: () => _organizations.active?.id,
          current: current,
          authorLayers: _layers,
          onOpenAuthorScope: authorCurrent() ? readAuthor : null,
          onLeaveAuthorScope: leaveAuthor,
          onAuthorMapVisible: (visible) {
            if (!authorCurrent() || _layers.authorID == null) return;
            setState(() => _publicPlacePanelMapVisible = visible);
          },
          onSelectAuthorMoment: (moment) {
            if (!authorCurrent() ||
                _layers.find(moment.mapID)?.sourceVersion !=
                    moment.sourceVersion) {
              return;
            }
            _workspace.selectEntity(moment.mapID, preserveViewport: true);
          },
          authorSelectionChanges: _workspace,
          selectedAuthorMomentID: () => _workspace.selectedEntityId,
          revalidate: () async {
            if (!current()) return false;
            final bounds = _layers.bounds;
            if (bounds == null) return false;
            await _layers.load(city, bounds);
            if (!current() || _layers.error != null) return false;
            final author = _layers.authorID, area = _layers.authorBounds;
            if (author != null && area != null) {
              return await _layers.loadAuthor(city, area, author) &&
                  authorCurrent();
            }
            return true;
          },
          onClose: closePanel,
          apiBaseUrl: base,
          client: _authenticatedTransport(client),
          onOpenActivity: (activity) =>
              _openFallbackActivity(activity, source: 'public_place'),
          onOpenOwnProfile: () async {
            if (!current() || !auth.signedIn || _organizations.active != null) {
              return;
            }
            await _destination(SidebarDestination.profile);
          },
        );
    final deadline = originatingAuthor == null
        ? _layers.publicView?.validUntil
        : _layers.authorView?.validUntil;
    if (deadline == null || !deadline.isAfter(DateTime.now().toUtc())) return;
    String? authorization() => auth.authorizationHeader;
    String? workspaceID() => _organizations.active?.id;
    Future<bool> revalidate() async {
      if (!current()) return false;
      final bounds = _layers.bounds;
      if (bounds == null) return false;
      await _layers.load(city, bounds);
      if (!current() || _layers.error != null) return false;
      if (originatingAuthor != null) {
        final area = _layers.authorBounds;
        if (area == null) return false;
        return await _layers.loadAuthor(city, area, originatingAuthor) &&
            current();
      }
      return true;
    }

    void openReader({String? momentID, bool directMoment = false}) {
      if (!current()) return;
      setState(() {
        _publicPlacePreview = false;
        _publicPlacePanel = reader(
          initialMomentID: momentID,
          directMoment: directMoment,
        );
      });
    }

    if (item.kind == 'MOMENT' && !openPlacePreview) {
      // Exact object detail in the existing map reader: no place-summary read,
      // no second navigator/map/composer and no hidden place frame prefetch.
      openReader(momentID: item.detailID, directMoment: true);
      return;
    }
    setState(() {
      _publicPlacePreview = true;
      _publicPlacePanel = NowPlacePreview(
        key: UniqueKey(),
        placeID: item.placeID,
        placeTitle: item.anchorLabel,
        validUntil: deadline,
        authorizationHeader: authorization,
        workspaceID: workspaceID,
        sourceChanges: changes,
        current: current,
        revalidate: revalidate,
        onClose: closePanel,
        onExpandedChanged: (expanded) {
          if (mounted &&
              _publicPlacePreview &&
              identical(_publicPlacePanelSource, source) &&
              (!expanded || current())) {
            setState(() => _publicPlaceStoriesExpanded = expanded);
          }
        },
        onOpenPlace: () => openReader(),
        onOpenMoment: (id) => openReader(momentID: id),
        apiBaseUrl: base,
        client: _authenticatedTransport(client),
      );
    });
  }

  void _openPlaceDetails(String placeID) {
    // The route can rebuild for keyboard, theme and text size. Keep its actual
    // providers stable; a genuine connection/owner replacement retires it.
    final auth = widget.auth;
    final owner = auth.accountID;
    final client = widget.placeDetailClient;
    final base = widget.placeDetailApiBaseUrl;
    final changes = Listenable.merge([
      auth,
      _organizations,
      _placeEntryChanges,
    ]);
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        owner == auth.accountID &&
        identical(client, widget.placeDetailClient) &&
        base == widget.placeDetailApiBaseUrl;
    String? authorization() => auth.authorizationHeader;
    String? workspaceID() => _organizations.active?.id;
    if (!current()) return;
    final String? Function() photoOwnerID = () =>
        current() ? auth.accountID : null;
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Theme.of(context).scaffoldBackgroundColor,
      builder: (sheetContext) => FractionallySizedBox(
        heightFactor: 0.72,
        child: NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => PlaceDetailSheet(
            onOpenMoment: (id) => openChatEntity(
              context,
              ChatEntityCard(type: 'moment', id: id, available: true),
              auth: auth,
              workspaceChanges: changes,
              workspaceID: workspaceID,
            ),
            placeID: placeID,
            client: _authenticatedTransport(client),
            apiBaseUrl: base,
            authorizationHeader: authorization,
            organizationWorkspaceID: workspaceID,
            ownerID: photoOwnerID,
            workspaceChanges: changes,
            onOpenOrganization: (organizationID) {
              if (!current()) return;
              Navigator.pop(sheetContext);
              unawaited(_openPublicOrganization(organizationID));
            },
            onOpenBusiness: (businessID) {
              if (!current()) return;
              Navigator.pop(sheetContext);
              unawaited(_openPublicBusiness(businessID));
            },
            onOpenActivity: (activity) {
              if (!current()) return;
              Navigator.pop(sheetContext);
              _openFallbackActivity(activity, source: 'place');
            },
          ),
        ),
      ),
    );
  }

  void _openFallbackActivity(PublicActivity activity, {String source = 'now'}) {
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Theme.of(context).colorScheme.surface,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(26)),
      ),
      builder: (context) => ActivityDetailSheet(
        accountID: () => widget.auth.accountID,
        apiBaseUrl: widget.seedApiBaseUrl,
        client: _authenticatedTransport(widget.seedClient),
        identityChanges: Listenable.merge([widget.auth, _organizations]),
        workspaceID: () => _organizations.active?.id,
        activity: activity,
        entrySource: source,
        authorizationHeader: () => widget.auth.authorizationHeader,
        onParticipationChanged: () => unawaited(_participations.load()),
        onOpenOrganization: (organizationID) {
          Navigator.of(context).pop();
          unawaited(_openPublicOrganization(organizationID));
        },
        onOpenOrganizer: (organizer) {
          Navigator.of(context).pop();
          unawaited(_openActivityOrganizer(organizer));
        },
        saved: _saved,
      ),
    );
  }

  Future<void> _openPublicOrganization(String organizationID) async {
    if (!mounted) return;
    final auth = widget.auth;
    String? ownerID() => identical(auth, widget.auth) ? auth.accountID : null;
    void openMap(String id) => unawaited(_openOrganizationMapControl(id));
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (context) => PublicOrganizationPage(
          organizationID: organizationID,
          client: _authenticatedTransport(widget.seedClient),
          apiBaseUrl: widget.seedApiBaseUrl,
          authorizationHeader: () => widget.auth.authorizationHeader,
          workspaceID: () => _organizations.active?.id,
          identityChanges: Listenable.merge([widget.auth, _organizations]),
          ownerID: ownerID,
          onOpenMapLocation: openMap,
          onOpenActivity: (activity) =>
              _openFallbackActivity(activity, source: 'organization'),
        ),
      ),
    );
  }

  Future<void> _openOrganizationMapControl(String organizationID) async {
    final auth = widget.auth, city = widget.city;
    final owner = auth.accountID, source = _personalSourceSerial;
    final selected = city.selectedCity;
    final client = widget.seedClient, base = widget.seedApiBaseUrl;
    if (!mounted ||
        owner == null ||
        !auth.signedIn ||
        _organizations.active != null)
      return;
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        identical(city, widget.city) &&
        auth.accountID == owner &&
        auth.signedIn &&
        source == _personalSourceSerial &&
        _organizations.active == null &&
        city.selectedCity?.id == selected?.id &&
        identical(client, widget.seedClient) &&
        base == widget.seedApiBaseUrl;
    final changes = Listenable.merge([
      auth,
      city,
      _organizations,
      _seedEntryChanges,
    ]);
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => OrganizationMapLocationPage(
            organizationID: organizationID,
            cityID: selected?.id ?? '',
            city: selected,
            citySource: () => city.selectedCity,
            authorizationHeader: () =>
                current() ? auth.authorizationHeader : null,
            ownerID: () => current() ? owner : null,
            sourceCurrent: current,
            identityChanges: changes,
            client: _authenticatedTransport(client),
            apiBaseUrl: base,
          ),
        ),
      ),
    );
    if (current()) await city.loadOrganizationPins();
  }

  Future<void> _openPublicBusiness(String businessID) async {
    if (!mounted) return;
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => PublicBusinessPage(
          businessID: businessID,
          authorizationHeader: () => widget.auth.authorizationHeader,
          workspaceID: () => _organizations.active?.id,
          identityChanges: Listenable.merge([widget.auth, _organizations]),
          apiBaseUrl: widget.placeDetailApiBaseUrl,
          client: _authenticatedTransport(widget.placeDetailClient),
          onOpenActivity: (activity) =>
              _openFallbackActivity(activity, source: 'business'),
        ),
      ),
    );
  }

  Future<void> _openActivityOrganizer(PublicActivityOrganizer organizer) async {
    if (!mounted) return;
    switch (organizer.type) {
      case 'BUSINESS':
        await _openPublicBusiness(organizer.id);
        return;
      case 'ORGANIZATION':
        await _openPublicOrganization(organizer.id);
        return;
      case 'COMMUNITY':
        final api = CommunityApi(
          authorizationHeader: () => widget.auth.authorizationHeader,
          client: _authenticatedTransport(widget.seedClient),
          apiBaseUrl: widget.seedApiBaseUrl,
        );
        try {
          await Navigator.of(context).push(
            MaterialPageRoute<void>(
              builder: (_) => CommunityDetailPage(
                id: organizer.id,
                api: api,
                auth: widget.auth,
                city: widget.city,
                organizations: _organizations,
              ),
            ),
          );
        } finally {
          api.dispose();
        }
        return;
      case 'PERSON':
        final auth = widget.auth;
        final client = widget.seedClient;
        final base = widget.seedApiBaseUrl;
        final serial = _personalSourceSerial;
        await Navigator.of(context).push(
          MaterialPageRoute<void>(
            builder: (_) => PublicPersonPage(
              accountID: organizer.id,
              auth: auth,
              client: _authenticatedTransport(client),
              apiBaseUrl: base,
              identityChanges: Listenable.merge([widget.auth, _organizations]),
              workspaceID: () => _organizations.active?.id,
              authorizationHeader: () => widget.auth.authorizationHeader,
              onOpenOwnProfile: () async {
                if (!mounted ||
                    serial != _personalSourceSerial ||
                    auth != widget.auth ||
                    client != widget.seedClient ||
                    base != widget.seedApiBaseUrl ||
                    auth.accountID != organizer.id ||
                    !auth.signedIn ||
                    _organizations.active != null) {
                  return;
                }
                await _destination(SidebarDestination.profile);
              },
            ),
          ),
        );
        return;
    }
  }

  Future<void> _openFriendManagement() async {
    if (!widget.auth.signedIn ||
        widget.auth.sessionRestoring ||
        _organizations.active != null)
      return;
    _pauseComposer();
    final source = _socialSource;
    final identity = _currentSeedIdentity,
        epoch = _seedEntryEpoch,
        auth = widget.auth,
        client = source.connections.followClient,
        base = source.connections.followApiBaseUrl;
    bool current() =>
        mounted &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        auth == widget.auth &&
        identical(source, _socialSource) &&
        _socialCurrent() &&
        auth.signedIn &&
        !auth.sessionRestoring &&
        _organizations.active == null;
    final changes = Listenable.merge([
      auth,
      _organizations,
      _seedEntryChanges,
      _socialProviderChanges,
    ]);
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          current: current,
          identityChanges: changes,
          builder: (inner) => Scaffold(
            appBar: birdtieAppBar(inner, title: const Text('好友管理')),
            body: ListView(
              padding: const EdgeInsets.all(16),
              children: [
                SocialInboxSection(
                  auth: auth,
                  workspaceChanges: changes,
                  workspaceID: () => _organizations.active?.id,
                  client: _authenticatedTransport(client),
                  apiBaseUrl: base,
                ),
              ],
            ),
          ),
        ),
      ),
    );
    if (current()) {
      _seedEntryChanges.value++;
      _systemPushRefresh.value++;
    }
  }

  Future<void> _openAgentActivityJoin(
    AgentResult source,
    AgentResultItem item,
  ) async {
    final owner = widget.auth.accountID;
    final token = widget.auth.authorizationHeader;
    final authSerial = _authSerial;
    final workspaceSerial = _workspaceSerial;
    if (!mounted ||
        owner == null ||
        token == null ||
        _organizations.active != null)
      return;
    final entry = AgentActivityJoinEntry.capture(
      _workspace,
      source,
      item,
      owner,
    );
    if (entry == null) return;
    final auth = widget.auth;
    final String? Function() authorization = () => auth.authorizationHeader;
    final String? Function() account = () => auth.accountID;
    final String? Function() organization = () => _organizations.active?.id;
    final transport = _authenticatedTransport(widget.seedClient);
    final base = widget.seedApiBaseUrl;
    final selection = entry.selection;
    final changes = Listenable.merge([
      _workspace,
      _organizations,
      widget.auth,
      entry,
    ]);
    bool current() =>
        mounted &&
        entry.current &&
        _authSerial == authSerial &&
        _workspaceSerial == workspaceSerial &&
        identical(widget.auth, auth) &&
        widget.auth.accountID == owner &&
        widget.auth.authorizationHeader == token &&
        _organizations.active == null;
    try {
      await Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => AgentActivityJoinPage(
            authorizationHeader: authorization,
            ownerID: account,
            organizationWorkspaceID: organization,
            sourceCurrent: current,
            identityChanges: changes,
            client: transport,
            apiBaseUrl: base,
            selection: selection,
          ),
        ),
      );
      if (current()) unawaited(_plans.load());
    } finally {
      entry.dispose();
    }
  }

  Future<void> _openActivityByID(
    String activityID, {
    String source = 'direct',
  }) async {
    const apiBase = BirdtieEnvironment.apiBaseUrl;
    if (apiBase.isEmpty) return;
    final token = widget.auth.authorizationHeader;
    final serial = _authSerial;
    final workspaceSerial = _workspaceSerial;
    final workspace = _organizations.active?.id;
    bool current() =>
        _currentAccount(serial, token) &&
        workspaceSerial == _workspaceSerial &&
        workspace == _organizations.active?.id;
    try {
      final response = await http
          .get(
            Uri.parse(
              '${apiBase.replaceFirst(RegExp(r'/$'), '')}/v1/activities/${Uri.encodeComponent(activityID)}',
            ),
            headers: {'Authorization': ?token},
          )
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('Activity unavailable');
      if (!current()) return;
      final activity = PublicActivity.fromJson(
        (jsonDecode(response.body) as Map<String, dynamic>)['data']
            as Map<String, dynamic>,
      );
      if (activity.id != activityID) throw const FormatException('活动标识不符');
      if (current()) {
        _openFallbackActivity(activity, source: source);
      }
    } catch (_) {
      if (mounted && current()) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('活动详情暂不可用，请稍后重试。')));
      }
    }
  }

  Future<void> _openConversationByID(String conversationID) async {
    if (_friendConversation?.audioActive == true) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先停止或取消当前录音，再切换接收人。')));
      return;
    }
    final source = _socialSource;
    final auth = widget.auth;
    final selection = ++_socialSelectionSerial;
    final token = widget.auth.authorizationHeader,
        serial = _authSerial,
        workspaceSerial = _workspaceSerial,
        workspace = _organizations.active?.id;
    bool current() =>
        mounted &&
        selection == _socialSelectionSerial &&
        _socialCurrent() &&
        identical(source, _socialSource) &&
        auth == widget.auth &&
        _currentAccount(serial, token) &&
        workspaceSerial == _workspaceSerial &&
        workspace == _organizations.active?.id &&
        workspace == null;
    if (!current()) return;
    setState(() => _socialExpanded = true);
    try {
      final target = await source.connections.conversation(conversationID);
      if (!mounted || !current() || !_socialExpanded) return;
      _selectFriendConversation(target);
    } catch (_) {
      if (mounted && current() && _socialExpanded) {
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('对话暂不可用，请稍后重试。')));
      }
    }
  }

  void _selectFriendConversation(HumanConversation target) {
    if (!_socialCurrent() ||
        _organizations.active != null ||
        !widget.auth.signedIn ||
        _friendConversation?.audioActive == true)
      return;
    _friendConversation?.retireImageView();
    final source = _socialSource.connections;
    final cached = _friendConversations.containsKey(target.id);
    final controller = _friendConversations.putIfAbsent(target.id, () {
      final value = FriendConversationController(
        auth: widget.auth,
        conversation: target,
        audioPendingStore: widget.friendAudioPendingStore,
        messagePendingStore: widget.humanMessagePendingStore,
        imageEnabled: widget.friendImageEnabled,
        imagePicker: widget.friendImagePicker,
        imagePendingStore: widget.friendImagePendingStore,
        imageSourceBinding: () => _socialSource,
        client: _authenticatedTransport(source.followClient),
        apiBaseUrl: source.followApiBaseUrl,
        workspaceChanges: Listenable.merge([
          _socialChanges,
          _socialProviderChanges,
        ]),
        workspaceID: () => _organizations.active?.id,
        onUnauthorized: (token) => unawaited(_handleRejectedSession(token)),
        isVisible: () =>
            mounted &&
            _socialCurrent() &&
            ModalRoute.of(context)?.isCurrent == true &&
            WidgetsBinding.instance.lifecycleState ==
                AppLifecycleState.resumed &&
            _socialExpanded &&
            _socialSection == NowSocialSection.messages &&
            _composerDrafts.target.conversationID == target.id,
      );
      value.addListener(_taskImagesChanged);
      unawaited(value.initialise());
      return value;
    });
    if (!controller.current) return;
    _socialSelectionSerial++;
    _composerKey.currentState?.pauseEditing();
    setState(() {
      _composerDrafts.select(NowComposerTarget.human(target));
      _humanConversationExpanded = false;
      _socialExpanded = true;
      _socialSection = NowSocialSection.messages;
      _socialRequestScope = null;
    });
    if (cached) unawaited(controller.load());
  }

  Future<void> _openFriendConversation(FriendTie friend) async {
    if (!_socialCurrent() ||
        !_socialExpanded ||
        _organizations.active != null ||
        _friendConversation?.audioActive == true)
      return;
    final source = _socialSource, identity = _currentSeedIdentity;
    final epoch = _seedEntryEpoch;
    final selection = ++_socialSelectionSerial;
    try {
      final conversation = await source.connections.startFriendChat(friend.id);
      if (!mounted ||
          selection != _socialSelectionSerial ||
          !_socialExpanded ||
          !identical(source, _socialSource) ||
          identity != _currentSeedIdentity ||
          epoch != _seedEntryEpoch ||
          !_socialCurrent())
        return;
      if (conversation.otherAccountId != friend.otherAccountId) {
        throw const FormatException('好友会话对象不符');
      }
      _selectFriendConversation(conversation);
    } catch (_) {
      if (mounted &&
          selection == _socialSelectionSerial &&
          _socialExpanded &&
          identity == _currentSeedIdentity &&
          epoch == _seedEntryEpoch)
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('暂时无法开启对话，请重试。')));
    }
  }

  void _exitFriendConversation() {
    if (_friendConversation?.audioActive == true) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先停止或取消录音，录音和草稿会保留。')));
      return;
    }
    _friendConversation?.retireImageView();
    _composerKey.currentState?.pauseEditing();
    _socialSelectionSerial++;
    setState(() {
      _composerDrafts.select(const NowComposerTarget.agent());
      _humanConversationExpanded = false;
    });
  }

  Widget _humanConversationPanel(FriendConversationController controller) =>
      LayoutBuilder(
        builder: (context, constraints) {
          // A landscape IME can leave only the input line. Keep the recipient
          // draft and reading state without overflowing its bounded surface.
          if (constraints.maxHeight < 56) return const SizedBox.shrink();
          return Column(
            key: const Key('now-human-conversation-panel'),
            children: [
              Row(
                children: [
                  IconButton(
                    key: const Key('edge-back-messages'),
                    tooltip: '返回联系人',
                    onPressed: _exitFriendConversation,
                    icon: const Icon(Icons.arrow_back, size: 20),
                  ),
                  Expanded(
                    child: Semantics(
                      header: true,
                      child: Text(
                        controller.conversation.otherName,
                        style: NowUiTokens.of(context).placeTitle,
                      ),
                    ),
                  ),
                  IconButton(
                    tooltip: _humanConversationExpanded ? '收起聊天' : '展开聊天',
                    onPressed: () => setState(
                      () => _humanConversationExpanded =
                          !_humanConversationExpanded,
                    ),
                    icon: Icon(
                      _humanConversationExpanded
                          ? Icons.unfold_less
                          : Icons.unfold_more,
                      size: 20,
                    ),
                  ),
                  IconButton(
                    tooltip: '返回 Now',
                    key: const Key('now-human-close'),
                    onPressed: _closeSocial,
                    icon: const Icon(Icons.close, size: 20),
                  ),
                ],
              ),
              Expanded(
                child: FriendConversationThread(
                  key: ValueKey(controller),
                  controller: controller,
                  showRecipient: false,
                  imageCaption: () => _composerDrafts.value.text,
                  onImageOperation: _imageJournalAction,
                  onOpenEntity: (entity) => openChatEntity(
                    context,
                    entity,
                    auth: widget.auth,
                    workspaceChanges: _socialChanges,
                    workspaceID: () => _organizations.active?.id,
                  ),
                ),
              ),
            ],
          );
        },
      );

  Future<void> _createOrganization() async {
    if (!widget.auth.signedIn) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先登录再创建组织工作区。')));
      return;
    }
    final token = widget.auth.authorizationHeader;
    final serial = _authSerial;
    final name = TextEditingController();
    var type = 'student_society';
    var saving = false;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) {
        _confirmationContext = dialogContext;
        return StatefulBuilder(
          builder: (dialogContext, update) => AlertDialog(
            title: const Text('创建组织工作区'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                TextField(
                  controller: name,
                  maxLength: 160,
                  decoration: const InputDecoration(labelText: '组织名称'),
                ),
                DropdownButtonFormField<String>(
                  initialValue: type,
                  decoration: const InputDecoration(labelText: '组织类型'),
                  items: const [
                    DropdownMenuItem(
                      value: 'student_society',
                      child: Text('学生社团'),
                    ),
                    DropdownMenuItem(value: 'club', child: Text('俱乐部')),
                    DropdownMenuItem(value: 'business', child: Text('商家')),
                    DropdownMenuItem(value: 'university', child: Text('高校')),
                    DropdownMenuItem(value: 'community', child: Text('社区组织')),
                    DropdownMenuItem(value: 'venue', child: Text('场所')),
                    DropdownMenuItem(value: 'nonprofit', child: Text('非营利组织')),
                    DropdownMenuItem(value: 'other', child: Text('其他')),
                  ],
                  onChanged: (value) {
                    if (value != null) update(() => type = value);
                  },
                ),
                const SizedBox(height: 8),
                Text(
                  '你的账号将成为首位所有者。组织工作区的 Agent 权限与个人数据分开管理。',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
            actions: [
              TextButton(
                onPressed: saving ? null : () => Navigator.pop(dialogContext),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: saving
                    ? null
                    : () async {
                        if (!_currentAccount(serial, token)) return;
                        update(() => saving = true);
                        var created = false;
                        try {
                          await _organizations.create(name.text.trim(), type);
                          created = true;
                          if (_currentAccount(serial, token) &&
                              dialogContext.mounted) {
                            Navigator.pop(dialogContext);
                          }
                        } catch (_) {
                          if (mounted && _currentAccount(serial, token)) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(content: Text('创建组织工作区失败，请重试。')),
                            );
                          }
                        } finally {
                          if (!created && dialogContext.mounted) {
                            update(() => saving = false);
                          }
                        }
                      },
                child: const Text('创建'),
              ),
            ],
          ),
        );
      },
    );
    if (_currentAccount(serial, token)) _confirmationContext = null;
    // showDialog completes before the reverse transition removes its TextField.
    await Future<void>.delayed(const Duration(milliseconds: 350));
    name.dispose();
  }

  Future<void> _viewOrganizationInvitations() async {
    if (!widget.auth.signedIn) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先登录再查看组织邀请。')));
      return;
    }
    final auth = widget.auth, owner = widget.auth.accountID;
    final serial = _personalSourceSerial;
    final client = widget.seedClient, base = widget.seedApiBaseUrl;
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        auth.signedIn &&
        auth.accountID == owner &&
        serial == _personalSourceSerial &&
        identical(client, widget.seedClient) &&
        base == widget.seedApiBaseUrl;
    final changes = Listenable.merge([
      auth,
      _organizations,
      widget.city,
      _seedEntryChanges,
    ]);
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (context) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => OrganizationInvitationsPage(
            auth: widget.auth,
            onAccepted: () async {
              if (current()) await _organizations.load();
            },
            client: _authenticatedTransport(client),
            apiBaseUrl: base,
            sourceCurrent: current,
          ),
        ),
      ),
    );
  }

  Future<void> _requestContact(AgentPerson person) async {
    if (!widget.auth.signedIn) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请先登录再申请联系。')));
      return;
    }
    final cityID = widget.city.selectedCity?.id;
    if (cityID == null) return;
    final token = widget.auth.authorizationHeader;
    final serial = _authSerial;
    final account = widget.auth.accountID;
    final workspace = _organizations.active?.id;
    final source = _connections;
    bool current() =>
        _currentAccount(serial, token) &&
        widget.auth.accountID == account &&
        _organizations.active?.id == workspace &&
        widget.city.selectedCity?.id == cityID;
    final note = TextEditingController();
    var sending = false;
    var requestFriend = true;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) {
        _confirmationContext = dialogContext;
        return StatefulBuilder(
          builder: (dialogContext, update) => AlertDialog(
            scrollable: true,
            title: Text('联系 ${person.displayName}'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Text('对方可以选择是否接受。你的留言和显示名称会分享给对方。'),
                SegmentedButton<bool>(
                  segments: const [
                    ButtonSegment(value: true, label: Text('加为好友')),
                    ButtonSegment(value: false, label: Text('申请私信')),
                  ],
                  selected: {requestFriend},
                  onSelectionChanged: sending
                      ? null
                      : (value) => update(() => requestFriend = value.first),
                ),
                Text(requestFriend ? '建立持续的好友关系，不会自动开启私信。' : '仅请求开启这次对话。'),
                TextField(
                  controller: note,
                  maxLength: 280,
                  maxLines: 3,
                  decoration: const InputDecoration(labelText: '留言'),
                ),
              ],
            ),
            actions: [
              TextButton(
                onPressed: sending ? null : () => Navigator.pop(dialogContext),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: sending
                    ? null
                    : () async {
                        if (!_currentAccount(serial, token)) return;
                        final body = note.text.trim();
                        if (body.isEmpty) return;
                        update(() => sending = true);
                        try {
                          final isFriend = requestFriend;
                          var submitted = false;
                          await runEntityAction(
                            context,
                            ref: EntityActionRef('person', person.accountID),
                            kind: EntityActionKind.connect,
                            operation: isFriend
                                ? 'REQUEST_FRIEND'
                                : 'REQUEST_CONVERSATION',
                            authorizationHeader: () => current() ? token : null,
                            accountID: () => current() ? account : null,
                            workspaceID: () => _organizations.active?.id,
                            identityChanges: _activeIntentChanges,
                            client: _authenticatedTransport(
                              source.followClient,
                            ),
                            apiBaseUrl: source.followApiBaseUrl,
                            domainCurrent: current,
                            reviewDetails: (_) =>
                                '留言：$body\n${isFriend ? '申请持续好友关系' : '申请本次私信；当前城市：$cityID'}',
                            handler: (approved) async {
                              if (!current()) return;
                              if (isFriend) {
                                await source.requestFriend(
                                  person.accountID,
                                  body,
                                  approved: approved,
                                );
                              } else {
                                await source.request(
                                  person.accountID,
                                  cityID,
                                  body,
                                  approved: approved,
                                );
                              }
                              submitted = true;
                            },
                          );
                          if (!submitted) return;
                          if (current() && dialogContext.mounted) {
                            Navigator.pop(dialogContext);
                          }
                          if (mounted && _currentAccount(serial, token)) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              SnackBar(
                                content: Text(
                                  '${requestFriend ? '好友' : '私信'}申请已发送，请在收件箱查看回复。',
                                ),
                              ),
                            );
                          }
                        } catch (_) {
                          if (mounted && _currentAccount(serial, token)) {
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(
                                content: Text('申请结果暂未确认，请先检查收件箱；不会自动重新发送。'),
                              ),
                            );
                          }
                        } finally {
                          if (dialogContext.mounted) {
                            update(() => sending = false);
                          }
                        }
                      },
                child: const Text('检查申请'),
              ),
            ],
          ),
        );
      },
    );
    if (_currentAccount(serial, token)) _confirmationContext = null;
    await Future<void>.delayed(const Duration(milliseconds: 350));
    note.dispose();
  }

  Future<void> _openSavedItem(SavedItem item) async {
    final type = switch (item.kind) {
      'place' => 'place',
      'activity' => 'activity',
      'group' => 'community',
      _ => '',
    };
    final id = item.targetId;
    if (!item.available ||
        type.isEmpty ||
        !chatEntityUUID.hasMatch(id) ||
        !widget.auth.signedIn ||
        widget.auth.sessionRestoring ||
        _saved.authorizationHeader() == null) {
      return;
    }
    final auth = widget.auth, saved = _saved;
    final identity = _currentSeedIdentity, epoch = _seedEntryEpoch;
    final changes = _activeIntentChanges;
    bool current() =>
        mounted &&
        auth == widget.auth &&
        saved == _saved &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch;
    if (!current()) return;
    String? currentWorkspaceID() => _organizations.active?.id;
    final workspaceID = currentWorkspaceID;
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: changes,
          current: current,
          builder: (_) => ChatEntityDetail(
            type: type,
            id: id,
            auth: auth,
            workspaceChanges: changes,
            workspaceID: workspaceID,
            apiBaseUrl: saved.followApiBaseUrl,
            client: _authenticatedTransport(saved.followClient),
          ),
        ),
      ),
    );
  }

  Future<void> _openFollowTarget(NowFollowEntry item) async {
    final type = switch (item.type) {
      'ORGANIZATION' => 'organization',
      'BUSINESS' => 'business',
      'COMMUNITY' => 'community',
      _ => '',
    };
    if (type.isEmpty ||
        !chatEntityUUID.hasMatch(item.targetID.toLowerCase()) ||
        !_socialCurrent())
      return;
    final auth = widget.auth, source = _socialSource, epoch = _seedEntryEpoch;
    final identity = _currentSeedIdentity;
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        identical(source, _socialSource) &&
        identity == _currentSeedIdentity &&
        epoch == _seedEntryEpoch &&
        _socialCurrent();
    await Navigator.of(context).push<void>(
      MaterialPageRoute(
        builder: (_) => NotificationDestinationBoundary(
          identityChanges: _activeIntentChanges,
          current: current,
          builder: (_) => ChatEntityDetail(
            type: type,
            id: item.targetID.toLowerCase(),
            auth: auth,
            workspaceChanges: _activeIntentChanges,
            workspaceID: () => _organizations.active?.id,
            client: _authenticatedTransport(source.connections.followClient),
            apiBaseUrl: source.connections.environment,
          ),
        ),
      ),
    );
  }

  Future<void> _addSavedToCollection(List<SavedItem> items) async {
    if (items.isEmpty || !widget.auth.signedIn || _organizations.active != null)
      return;
    final auth = widget.auth,
        saved = _saved,
        owner = auth.accountID,
        source = _personalSourceSerial;
    final workspace = _organizations.active?.id;
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        identical(saved, _saved) &&
        auth.accountID == owner &&
        auth.signedIn &&
        !auth.sessionRestoring &&
        _personalSourceSerial == source &&
        _organizations.active?.id == workspace;
    final c = SavedCollectionAppendController(
      saved: saved,
      selected: List.unmodifiable(items),
      ownerID: () => current() ? owner : null,
      sourceCurrent: current,
      identityChanges: Listenable.merge([
        auth,
        saved,
        _organizations,
        _seedEntryChanges,
      ]),
    );
    try {
      await Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => SavedCollectionAppendPage(controller: c),
        ),
      );
    } finally {
      c.dispose();
    }
  }

  Future<void> _openDraftContext() async {
    _pauseComposer();
    _syncLocalMedia();
    if (!mounted ||
        _composerDrafts.suspended ||
        _composerDrafts.envelope == null)
      return;
    final identity = _composerDrafts.identity;
    final changes = Listenable.merge([
      widget.auth,
      _organizations,
      _seedEntryChanges,
      _workspace,
      _composerDrafts,
    ]);
    bool current() =>
        mounted &&
        !_composerDrafts.suspended &&
        _composerDrafts.identity == identity;
    List<LocalDraftReferenceCandidate> candidates() {
      if (!current()) return const [];
      final result = _workspace.result, taskEpoch = _workspace.taskEpoch;
      if (result == null || !result.replyProjectionCurrent) return const [];
      return [
        for (final item in result.projectionItems ?? const <AgentResultItem>[])
          if (item.detail == item.entity &&
              item.sourceVersion.isNotEmpty &&
              item.actionsValidUntil != null)
            LocalDraftReferenceCandidate(
              kind: item.entity.type,
              id: item.entity.id,
              title: item.title,
              version: item.sourceVersion,
              expiresAt: item.actionsValidUntil!,
              sourceCurrent: () =>
                  current() &&
                  identical(_workspace.result, result) &&
                  _workspace.taskEpoch == taskEpoch &&
                  result.replyProjectionCurrent,
            ),
      ];
    }

    final navigator = Navigator.of(context);
    late final MaterialPageRoute<void> contextRoute;
    contextRoute = MaterialPageRoute<void>(
      builder: (_) => LocalDraftContextPage(
        drafts: _composerDrafts,
        candidates: candidates,
        identityChanges: changes,
        attachmentCurrent: (attachment) {
          final binding = _localMediaBinding;
          return current() &&
              binding != null &&
              !_localMedia.displayNeedsReselect(binding, attachment);
        },
        canReselectAttachment: (attachment) =>
            current() &&
            attachment.kind == 'image' &&
            (attachment.state != 'unknown') &&
            (_composerDrafts.target.human
                ? _friendConversation?.canSelectImageSource(
                        MomentImageSource.gallery,
                      ) ==
                      true
                : _imageDraft?.canSelectSource(MomentImageSource.gallery) ==
                      true),
        onReselectAttachment: (attachment) async {
          if (!current() ||
              attachment.kind != 'image' ||
              attachment.state == 'unknown')
            return;
          final human = _composerDrafts.target.human;
          final ComposerImageDraftView? originalImage = human
              ? _friendConversation?.imageDraft
              : _imageDraft;
          final before = originalImage?.localBytes;
          if (!contextRoute.isCurrent ||
              !identical(contextRoute.navigator, navigator))
            return;
          navigator.pop();
          await contextRoute.completed;
          if (!current()) return;
          if (human) {
            await _friendConversation?.selectImage(
              source: MomentImageSource.gallery,
              gestureCurrent: current,
            );
          } else {
            await _openDraftImage();
          }
          if (!current()) return;
          final ComposerImageDraftView? image = human
              ? _friendConversation?.imageDraft
              : _imageDraft;
          if (image?.current != true ||
              image?.localBytes == null ||
              identical(before, image?.localBytes))
            return;
          _syncLocalMedia();
          // Re-selection replaces only the explicit historical local record;
          // the original upload/UNKNOWN journal belongs to its own writer.
          final rows = _composerDrafts.envelope!.attachments;
          final binding = _localMediaBinding;
          if (binding != null &&
              _localMedia.displayNeedsReselect(binding, attachment)) {
            _composerDrafts.setContext(
              attachments: rows.where((a) => a.id != attachment.id).toList(),
            );
          }
        },
      ),
    );
    await navigator.push(contextRoute);
    if (mounted) setState(() {});
  }

  Future<void> _openRouteDraft({bool importPlan = false}) async {
    _pauseComposer();
    final auth = widget.auth,
        city = widget.city,
        chosen = widget.city.selectedCity;
    final owner = auth.accountID, source = _personalSourceSerial;
    final client = widget.seedClient, base = widget.seedApiBaseUrl;
    if (chosen == null ||
        owner == null ||
        !auth.signedIn ||
        _organizations.active != null ||
        _composerDrafts.target.human) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('请以个人身份登录并选择城市后编辑计划。')));
      return;
    }
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        identical(city, widget.city) &&
        auth.accountID == owner &&
        auth.signedIn &&
        _organizations.active == null &&
        city.selectedCity?.id == chosen.id &&
        source == _personalSourceSerial &&
        identical(client, widget.seedClient) &&
        base == widget.seedApiBaseUrl;
    final changes = Listenable.merge([
      auth,
      city,
      _organizations,
      _seedEntryChanges,
    ]);
    final c = RouteDraftController(
      origin: base ?? _saved.followApiBaseUrl,
      owner: owner,
      cityID: chosen.id,
      saved: _saved,
      boundaryChanges: changes,
      sourceCurrent: current,
      initial: _composerDrafts.derive('route'),
      localOwnerCurrent: () =>
          identical(auth, widget.auth) &&
          (auth.accountID == owner || auth.rejectedSessionOwnerID == owner),
    );
    try {
      if (_saved.items.isEmpty && !_saved.loading)
        await _saved.load(paged: true);
      if (!current()) return;
      final selected = await Navigator.of(context).push<List<SavedItem>>(
        MaterialPageRoute(
          builder: (route) => NotificationDestinationBoundary(
            identityChanges: changes,
            current: current,
            builder: (_) => RouteDraftPage(
              controller: c,
              importOnOpen: importPlan,
              onMap: (rows) {
                if (current() && c.current) Navigator.pop(route, rows);
              },
              onAddToCollection: (rows) {
                if (current() && c.current)
                  unawaited(_addSavedToCollection(rows));
              },
            ),
          ),
        ),
      );
      await c.flush();
      if (selected != null && current() && c.current) {
        _showSavedOnMap(selected, title: '${c.value.label} · 仅地点，路线未计算');
      }
    } finally {
      c.dispose();
    }
  }

  void _showSavedOnMap(List<SavedItem> items, {String title = '收藏地点'}) {
    final city = widget.city.selectedCity, auth = widget.auth, saved = _saved;
    if (city == null || !auth.signedIn || _organizations.active != null) return;
    final usable = <String, SavedItem>{
      for (final i in items)
        if (i.mapCurrent && i.cityId == city.id) i.targetId: i,
    };
    if (usable.isEmpty) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('当前城市没有可上图的收藏地点。')));
      return;
    }
    final owner = auth.accountID, scope = _personalSourceSerial;
    bool current() =>
        mounted &&
        identical(auth, widget.auth) &&
        identical(saved, _saved) &&
        auth.accountID == owner &&
        auth.signedIn &&
        _organizations.active == null &&
        widget.city.selectedCity?.id == city.id &&
        _personalSourceSerial == scope;
    _savedMapSource?.dispose();
    _collectionProjectionExpiry?.cancel();
    late final SavedMapProjectionSource projection;
    projection = SavedMapProjectionSource(
      saved: saved,
      items: List.unmodifiable(usable.values),
      cityID: city.id,
      boundaryChanges: Listenable.merge([
        auth,
        widget.city,
        _organizations,
        _seedEntryChanges,
      ]),
      sourceCurrent: current,
      onRetired: () {
        if (mounted &&
            identical(_savedMapSource, projection) &&
            identical(_collectionProjection.value, projection.projection)) {
          _collectionProjection.value = null;
          WidgetsBinding.instance.addPostFrameCallback((_) {
            if (mounted) setState(() {});
          });
        }
      },
      title: title,
    );
    _savedMapSource = projection;
    setState(() => _collectionProjection.value = projection.projection);
    _collectionProjectionExpiry = Timer(
      projection.expiresAt.difference(DateTime.now().toUtc()),
      () {
        if (mounted && identical(_savedMapSource, projection))
          setState(() => _collectionProjection.value = null);
      },
    );
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('显示当前城市已加载的${usable.length}个地点，点选最多4项比较。')),
    );
  }

  Future<void> _compareSavedMap() async {
    final source = _savedMapSource;
    if (source == null || !source.current || source.comparison.length < 2)
      return;
    final captured = List<SavedItem>.unmodifiable(source.comparison);
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (_) => AnimatedBuilder(
        animation: Listenable.merge([_saved, _activeIntentChanges]),
        builder: (context, _) => SizedBox(
          height: MediaQuery.sizeOf(context).height * .75,
          child: ListView(
            padding: const EdgeInsets.all(16),
            children: [
              const Text('比较收藏地点'),
              if (!source.current)
                const Text('来源已变化，请返回重新读取。')
              else
                for (final item in captured)
                  ListTile(
                    title: Text(item.title),
                    subtitle: Text(item.summary),
                    onTap: () => unawaited(_openSavedItem(item)),
                  ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _destination(SidebarDestination destination) async {
    _pauseComposer();
    if (_scaffold.currentState?.isDrawerOpen == true) Navigator.pop(context);
    if (destination == SidebarDestination.home) return;
    final personalDestination =
        destination == SidebarDestination.profile ||
        destination == SidebarDestination.settings;
    final auth = widget.auth, moments = widget.moments;
    final client = widget.seedClient, base = widget.seedApiBaseUrl;
    final personalSourceSerial = _personalSourceSerial;
    bool currentPersonalSource() =>
        mounted &&
        personalSourceSerial == _personalSourceSerial &&
        auth == widget.auth &&
        moments == widget.moments &&
        client == widget.seedClient &&
        base == widget.seedApiBaseUrl;
    final title = switch (destination) {
      SidebarDestination.activities => '我的活动',
      SidebarDestination.organization => '组织活动管理',
      SidebarDestination.business => '商家工作台',
      SidebarDestination.groups => '社群',
      SidebarDestination.saved => '收藏',
      SidebarDestination.profile => '个人资料',
      SidebarDestination.settings => '设置',
      SidebarDestination.home => '首页',
    };
    final content = switch (destination) {
      SidebarDestination.activities => MyActivitiesPage(
        plans: _plans,
        participations: _participations,
        saved: _saved,
        auth: widget.auth,
        city: widget.city,
        organizations: _organizations,
      ),
      SidebarDestination.organization => OrganizationConsolePage(
        auth: widget.auth,
        city: widget.city,
        organizations: _organizations,
      ),
      SidebarDestination.business => BusinessConsolePage(
        auth: widget.auth,
        city: widget.city,
        organizations: _organizations,
      ),
      SidebarDestination.profile => LegacyProfilePage(
        auth: widget.auth,
        moments: widget.moments,
        city: widget.city,
        client: _authenticatedTransport(widget.seedClient),
        apiBaseUrl: widget.seedApiBaseUrl,
        workspaceChanges: _activeIntentChanges,
        organizationWorkspaceID: () => _organizations.active?.id,
        current: currentPersonalSource,
        onOpenSocialNow: () => unawaited(_openSocialNowRoute()),
      ),
      SidebarDestination.groups => CommunityPage(
        auth: widget.auth,
        city: widget.city,
        organizations: _organizations,
      ),
      SidebarDestination.saved => SavedPage(
        saved: _saved,
        cityNames: {for (final city in widget.city.cities) city.id: city.name},
        onOpenItem: (item) => unawaited(_openSavedItem(item)),
        onAddToCollection: _organizations.active == null
            ? (items) => unawaited(_addSavedToCollection(items))
            : null,
        onViewMap: _organizations.active == null
            ? (items) {
                Navigator.of(context).pop();
                _showSavedOnMap(items);
              }
            : null,
      ),
      SidebarDestination.settings => SettingsPage(
        auth: widget.auth,
        city: widget.city,
        moments: widget.moments,
        client: _authenticatedTransport(widget.seedClient),
        apiBaseUrl: widget.seedApiBaseUrl,
        workspaceChanges: _organizations,
        organizationWorkspaceID: () => _organizations.active?.id,
        systemPush: _systemPush,
      ),
      _ => Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Text(
            '$title 暂未开放。',
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ),
      ),
    };
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (routeContext) {
          Widget destinationPage(BuildContext pageContext) => Scaffold(
            appBar: birdtieAppBar(pageContext, title: Text(title)),
            body: content,
          );
          if (!personalDestination) return destinationPage(routeContext);
          return NotificationDestinationBoundary(
            identityChanges: _seedEntryChanges,
            current: currentPersonalSource,
            builder: destinationPage,
          );
        },
      ),
    );
  }

  Future<void> _openAgentAction(AgentAction action) async {
    if (action.type == 'OPEN_OPPORTUNITIES') {
      await Navigator.push(
        context,
        MaterialPageRoute<void>(
          builder: (_) => OpportunityPage(auth: widget.auth),
        ),
      );
      return;
    }
    if (action.type == 'OPEN_NEW_PEOPLE') {
      await Navigator.push(
        context,
        MaterialPageRoute<void>(
          builder: (_) => NewPeoplePage(auth: widget.auth),
        ),
      );
      return;
    }
    if (action.type == 'OPEN_RELATIONSHIP_CONTEXT') {
      await Navigator.push(
        context,
        MaterialPageRoute<void>(
          builder: (_) => AgentRelationshipPage(auth: widget.auth),
        ),
      );
      // Signal snapshots are never retained in the map or public result entities.
      return;
    }
    if (action.type == 'OPEN_PLACE' && action.targetID != null) {
      _openPlaceDetails(action.targetID!);
      return;
    }
    if (action.type != 'OPEN_ORGANIZATION_CONSOLE' || action.targetID == null) {
      return;
    }
    await _organizations.load();
    if (!mounted) return;
    OrganizationWorkspace? allowed;
    for (final item in _organizations.organizations) {
      if (item.id == action.targetID &&
          (item.role == 'owner' || item.role == 'admin')) {
        allowed = item;
        break;
      }
    }
    if (allowed == null) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('组织权限已变化，请重新登录或刷新后重试。')));
      return;
    }
    _organizations.select(allowed);
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (context) => Scaffold(
          appBar: birdtieAppBar(context, title: const Text('组织活动管理')),
          body: OrganizationConsolePage(
            auth: widget.auth,
            city: widget.city,
            organizations: _organizations,
          ),
        ),
      ),
    );
  }

  @override
  void didChangeMetrics() {
    if (!mounted) return;
    final keyboard = View.of(context).viewInsets.bottom;
    final closed = _previousKeyboard > 0 && keyboard == 0;
    _previousKeyboard = keyboard;
    if (closed) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && View.of(context).viewInsets.bottom == 0)
          _pauseComposer();
      });
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) unawaited(_composerDrafts.flush());
    _workspace.updateReplyRefreshVisibility();
    _layers.updateRefreshVisibility();
  }

  void _onTypedDraftRestore() {
    if (!mounted) return;
    _queueLocalMediaSync();
    final error = _composerDrafts.storageError;
    if (error != null && error != _lastDraftStorageError) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && ModalRoute.of(context)?.isCurrent == true) {
          ScaffoldMessenger.maybeOf(
            context,
          )?.showSnackBar(SnackBar(content: Text(error)));
        }
      });
    }
    _lastDraftStorageError = error;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  @override
  Widget build(BuildContext context) {
    _syncSystemPush();
    // Subscribe to the existing route; returning never reopens or submits.
    _workspace.updateReplyRefreshVisibility();
    _layers.updateRefreshVisibility();
    return PopScope(
      canPop: !_socialExpanded,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          if (_composerDrafts.target.human)
            _exitFriendConversation();
          else
            _closeSocial();
        }
      },
      child: _buildWorkspace(context),
    );
  }

  Widget _buildWorkspace(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([
      widget.auth,
      widget.city,
      _workspace,
      _mapState,
      _organizations,
      _discovery,
      _online,
      _layers,
      _selectedMapAnchor,
    ]),
    builder: (context, _) => Scaffold(
      key: _scaffold,
      resizeToAvoidBottomInset: false,
      onDrawerChanged: _sidebarTapTraceEnabled
          ? (opened) => _traceSidebar('drawer_changed', reportedOpened: opened)
          : null,
      drawer: Sidebar(
        workspace: _workspace,
        city: widget.city,
        onCitySelected: (id) {
          _selectPublicCity(id);
        },
        onNew: () {
          _retirePublicPlacePanel();
          ++_viewSerial;
          _pendingCityCurrent = null;
          _workspace.newTask();
          _retireImageDraft();
          Navigator.pop(context);
        },
        onDestination: _destination,
        onTools: () {
          _scaffold.currentState?.closeDrawer();
          unawaited(_openNowTools());
        },
        onWorkspaceSelected: () => _scaffold.currentState?.closeDrawer(),
        onRecent: (task) {
          _retireImageDraft();
          Navigator.pop(context);
          unawaited(_reopenTask(task));
        },
        organizations: _organizations,
        onCreateOrganization: _createOrganization,
        onViewInvitations: _viewOrganizationInvitations,
        signedIn: widget.auth.signedIn,
        sessionRestoring: widget.auth.sessionRestoring,
        onChooseCity: () {
          Navigator.pop(context);
          unawaited(_chooseCity());
        },
      ),

      body: LayoutBuilder(
        builder: (context, constraints) {
          final media = MediaQuery.of(context);
          final keyboard = media.viewInsets.bottom;
          final editing = _workspace.inputFocused || keyboard > 0;
          final hasCreation = _creationCurrent;
          final minimumComposerHeight = AgentComposer.minimumInputHeightOf(
            context,
          );
          final preferredComposerHeight = _composerHeight.clamp(
            minimumComposerHeight,
            double.infinity,
          );
          final topControlsHeight =
              _topControlsHeight ?? media.viewPadding.top + 48;
          final normalChromeBottom =
              topControlsHeight + 8 + _ornamentHeight + 8;
          final contentBottom =
              constraints.maxHeight -
              (keyboard > media.viewPadding.bottom
                  ? keyboard
                  : media.viewPadding.bottom);
          final compactChrome =
              editing &&
              contentBottom - normalChromeBottom < preferredComposerHeight + 48;
          final chromeBottom = compactChrome
              ? media.viewPadding.top
              : normalChromeBottom;
          final hasHumanConversation =
              _friendConversation?.current == true &&
              _socialExpanded &&
              _publicPlacePanel == null;
          final hasSheet =
              !hasCreation &&
              !hasHumanConversation &&
              _publicPlacePanel == null &&
              (!_socialExpanded || editing) &&
              _layers.authorID == null &&
              (_workspace.task != null || _workspace.areaOperation != null) &&
              _workspace.sheetExtent != AgentSheetExtent.hidden;
          final currentSelection = _workspace.selectedEntityId == null
              ? const <MapEntity>[]
              : _visibleEntities()
                    .where((e) => e.id == _workspace.selectedEntityId)
                    .take(1)
                    .toList();
          final showSelection =
              !hasCreation &&
              !hasHumanConversation &&
              !hasSheet &&
              _publicPlacePanel == null &&
              currentSelection.isNotEmpty &&
              !(_workspace.task == null &&
                  {
                    MapEntityKind.place,
                    MapEntityKind.moment,
                  }.contains(currentSelection.single.kind));
          final showLayerRecovery =
              !hasCreation &&
              !hasSheet &&
              !editing &&
              _publicPlacePanel == null &&
              !_socialExpanded &&
              _layers.error != null &&
              _layers.cityID == widget.city.selectedCity?.id;
          final browse =
              !hasCreation &&
              !hasHumanConversation &&
              !hasSheet &&
              _publicPlacePanel == null &&
              !(_socialExpanded && !editing) &&
              _layers.authorID == null &&
              _exploreExpanded;
          final hasContent =
              hasCreation ||
              hasHumanConversation ||
              hasSheet ||
              showSelection ||
              showLayerRecovery ||
              browse;
          final showSocial =
              !hasCreation &&
              _socialExpanded &&
              !hasHumanConversation &&
              (!editing || _composerDrafts.target.human) &&
              _publicPlacePanel == null;
          final expanded = hasCreation
              ? !_creationCollapsed
              : hasHumanConversation
              ? _humanConversationExpanded
              : hasSheet
              ? _workspace.sheetExtent == AgentSheetExtent.expanded ||
                    _workspace.sheetExtent == AgentSheetExtent.medium
              : browse;
          final measuredPresentation = (
            _workspace.result,
            _workspace.conversation.length,
            _workspace.areaOperation,
            _workspace.contentMode,
            hasContent,
            constraints.maxWidth,
            media.textScaler.scale(16),
          );
          if (_measuredPresentation != measuredPresentation) {
            _measuredPresentation = measuredPresentation;
            _resultContentHeight = null;
          }
          final layout = NowWorkspaceLayout.resolve(
            rootHeight: constraints.maxHeight,
            rootWidth: constraints.maxWidth,
            safeTop: media.viewPadding.top,
            safeBottom: media.viewPadding.bottom,
            keyboard: keyboard,
            chromeBottom: chromeBottom,
            composerHeight: preferredComposerHeight,
            resultContentHeight: hasCreation
                ? (_creationCollapsed ? 64 : 420)
                : _resultContentHeight,
            hasContent: hasContent,
            expanded: expanded,
            editing: editing,
            textScale: media.textScaler.scale(16) / 16,
            minimumComposerHeight: minimumComposerHeight,
          );
          final composerMaxHeight =
              (layout.contentHeight - 2 * layout.surfaceInset).clamp(
                0.0,
                layout.availableHeight,
              );
          final hasSearchArea =
              _online.selected == null && _mapState.searchAreaBounds != null;
          final visibleHeight = constraints.maxHeight - keyboard;
          final mapBottom =
              (constraints.maxHeight -
                      layout.bottom -
                      layout.surfaceHeight -
                      (hasSearchArea && !editing ? 56 : 8))
                  .clamp(chromeBottom, constraints.maxHeight);
          final mapRect = Rect.fromLTRB(
            16,
            chromeBottom,
            (constraints.maxWidth - 16).clamp(16, double.infinity),
            mapBottom,
          );
          final previewViewport = (mapBottom - chromeBottom).clamp(0.0, 260.0);
          final previewVisible = !editing && previewViewport >= 56;
          if (_mapContentRect.value != mapRect) {
            WidgetsBinding.instance.addPostFrameCallback((_) {
              if (mounted && _mapContentRect.value != mapRect)
                _mapContentRect.value = mapRect;
            });
          }
          return Stack(
            children: [
              Positioned.fill(
                child: Listener(
                  onPointerDown: (_) {
                    if (_workspace.inputFocused) _pauseComposer();
                    if (_socialExpanded &&
                        (!hasHumanConversation ||
                            _friendConversation?.audioActive == true))
                      _closeSocial();
                  },
                  child: _mapCanvas,
                ),
              ),
              if (_collectionProjection.value case final collection?)
                Positioned(
                  top: chromeBottom + 8,
                  left: 16,
                  right: 88,
                  child: Material(
                    color: Theme.of(context).colorScheme.surface,
                    borderRadius: BorderRadius.circular(12),
                    child: Row(
                      children: [
                        Expanded(
                          child: Padding(
                            padding: const EdgeInsets.only(left: 12),
                            child: Text(
                              collection.current
                                  ? '正在查看：${collection.title}'
                                  : '记录已变化，请重新打开',
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        ),
                        if (_savedMapSource != null &&
                            identical(_savedMapSource!.projection, collection))
                          IconButton(
                            tooltip: '比较已选收藏地点',
                            onPressed:
                                _savedMapSource?.current == true &&
                                    (_savedMapSource?.comparison.length ?? 0) >=
                                        2
                                ? () => unawaited(_compareSavedMap())
                                : null,
                            icon: const Icon(Icons.compare_arrows),
                          ),
                        IconButton(
                          tooltip: '返回原地图',
                          onPressed: () {
                            _collectionProjectionExpiry?.cancel();
                            _savedMapSource?.dispose();
                            _savedMapSource = null;
                            setState(() => _collectionProjection.value = null);
                          },
                          icon: const Icon(Icons.close),
                        ),
                      ],
                    ),
                  ),
                ),
              if (!compactChrome)
                Positioned(
                  top: 0,
                  left: 0,
                  right: 0,
                  child: _observeSidebarTraceSurface(
                    context,
                    TopControls(
                      key: _sidebarTraceHeader,
                      onHeightChanged: (height) {
                        if (!mounted || _topControlsHeight == height) return;
                        setState(() => _topControlsHeight = height);
                        _ornamentTop.value = height + 8;
                      },
                      onSidebar: () {
                        _traceSidebar('callback_before_pause');
                        _pauseComposer();
                        _traceSidebar('callback_after_pause');
                        _scaffold.currentState?.openDrawer();
                        _traceSidebar('callback_after_open');
                        if (_sidebarTapTraceEnabled) {
                          final scaffold = _scaffold.currentState;
                          WidgetsBinding.instance.addPostFrameCallback((_) {
                            if (!mounted ||
                                !identical(scaffold, _scaffold.currentState))
                              return;
                            _traceSidebar('callback_post_frame');
                          });
                        }
                      },
                      onInbox: _toggleSocial,
                      onNotifications: _toggleNotifications,
                      socialExpanded:
                          showSocial &&
                          _socialSection != NowSocialSection.notifications,
                      notificationsExpanded:
                          showSocial &&
                          _socialSection == NowSocialSection.notifications,
                      hasUnreadMessages: _socialMessageUnread > 0,
                      hasUnreadNotifications:
                          (_socialNotificationUnread ?? 0) > 0,
                      onTools: () => unawaited(_openNowTools()),
                      onContext: () => unawaited(_chooseCity()),
                      contextLabel: widget.city.selectedCity?.name ?? '未选城市',
                    ),
                  ),
                ),
              if (!hasCreation &&
                  !hasSheet &&
                  !browse &&
                  !editing &&
                  !showSocial &&
                  _publicPlacePanel == null &&
                  _layers.authorID == null &&
                  _layers.items.any(
                    (i) => {'PLACE', 'MOMENT'}.contains(i.kind),
                  ))
                Positioned(
                  left: 16,
                  right: 16,
                  bottom: layout.bottom + layout.surfaceHeight + 8,
                  child: Center(
                    child: _NowExploreHandle(
                      expanded: false,
                      onToggle: () => setState(() => _exploreExpanded = true),
                    ),
                  ),
                ),
              if (_layers.authorID != null &&
                  !editing &&
                  _publicPlacePanel == null)
                Positioned(
                  top: chromeBottom,
                  left: 16,
                  child: TextButton.icon(
                    style: TextButton.styleFrom(
                      minimumSize: const Size(48, 48),
                      backgroundColor: Theme.of(context).colorScheme.surface,
                      textStyle: NowUiTokens.of(context).action,
                    ),
                    onPressed: () => setState(_retirePublicPlacePanel),
                    icon: const Icon(Icons.close, size: 20),
                    label: const Text('TA 的公开动态'),
                  ),
                ),
              if (hasSearchArea &&
                  !editing &&
                  _publicPlacePanel == null &&
                  !showSocial)
                Positioned(
                  key: const Key('now-map-area-command-position'),
                  left: 0,
                  right: 8,
                  bottom: layout.bottom + layout.surfaceHeight + 8,
                  child: Center(
                    child: _NowMapAreaCommand(
                      loading:
                          _workspace.areaOperation?.status ==
                              AgentAreaStatus.loading &&
                          _workspace.areaOperation?.bounds ==
                              _mapState.searchAreaBounds,
                      onPressed: _searchThisArea,
                    ),
                  ),
                ),
              Positioned(
                key: const Key('now-task-overlay'),
                left: 0,
                right: 0,
                bottom: layout.bottom,
                height: layout.surfaceHeight,
                child: Material(
                  key: const Key('now-primary-surface'),
                  color: hasContent
                      ? Theme.of(context).colorScheme.surface
                      : Colors.transparent,
                  surfaceTintColor: Colors.transparent,
                  shape: RoundedRectangleBorder(
                    borderRadius: const BorderRadius.vertical(
                      top: Radius.circular(NowUiTokens.sheetCornerTop),
                    ),
                    side: hasContent
                        ? BorderSide(
                            color: NowUiTokens.materialEdge(
                              Theme.of(context),
                              highContrast: MediaQuery.highContrastOf(context),
                            ),
                            width: NowUiTokens.glassEdgeWidth,
                          )
                        : BorderSide.none,
                  ),
                  elevation: hasContent ? NowUiTokens.glassElevation : 0,
                  shadowColor: NowUiTokens.materialShadow(Theme.of(context)),
                  clipBehavior: Clip.antiAlias,
                  child: Padding(
                    padding: EdgeInsets.fromLTRB(
                      0,
                      layout.surfaceInset,
                      0,
                      layout.surfaceInset + layout.contentSafeBottom,
                    ),
                    child: Column(
                      children: [
                        Expanded(
                          key: const Key('now-primary-content'),
                          child: Stack(
                            fit: StackFit.expand,
                            children: [
                              _RetainedMapContext(
                                visible: hasSheet && !hasHumanConversation,
                                child: LayoutBuilder(
                                  builder: (context, remaining) {
                                    final evidenceRouter = _agentResultRouter();
                                    return AgentResultsSheet(
                                      onReviewAgentJoin: (source, item) =>
                                          unawaited(
                                            _openAgentActivityJoin(
                                              source,
                                              item,
                                            ),
                                          ),
                                      attachmentBuilder: _taskImageMessage,
                                      workspace: _workspace,
                                      publicEvidenceCurrent:
                                          evidenceRouter.current,
                                      publicEvidenceChanges:
                                          evidenceRouter.workspaceChanges,
                                      saved: _saved,
                                      plans: _plans,
                                      onContact: _requestContact,
                                      onOpenActivity: (activity) =>
                                          _openFallbackActivity(
                                            activity,
                                            source: 'agent',
                                          ),
                                      onOpenPlace: _openPlaceDetails,
                                      onOpenEntity: (item) => unawaited(
                                        _agentResultRouter().open(
                                          context,
                                          item,
                                        ),
                                      ),
                                      onOpenReplyEntity: (source, item) =>
                                          unawaited(
                                            _agentResultRouter(
                                              reply: source,
                                            ).open(context, item),
                                          ),
                                      onFocusReplyPlace: _focusReplyPlace,
                                      onNavigateReplyPlace: (source, item) =>
                                          unawaited(
                                            _navigateReplyPlace(source, item),
                                          ),
                                      onShareReplyEntity: (source, item) =>
                                          unawaited(
                                            _agentResultRouter(
                                              reply: source,
                                            ).share(context, item),
                                          ),
                                      onSaveReplyEntity: (source, item) =>
                                          unawaited(
                                            _agentResultRouter(
                                              reply: source,
                                            ).save(context, item, _saved),
                                          ),
                                      onContentHeightChanged:
                                          _workspace.conversation.length <= 4
                                          ? _measureResultHeight
                                          : null,
                                      onShareEntity: (item) => unawaited(
                                        _agentResultRouter().share(
                                          context,
                                          item,
                                        ),
                                      ),
                                      onSaveEntity: (item) => unawaited(
                                        _agentResultRouter().save(
                                          context,
                                          item,
                                          _saved,
                                        ),
                                      ),
                                      onOpenOnlineIntent: (id) =>
                                          unawaited(_openOnlineIntent(id)),
                                      onActivityImpression: (activity) =>
                                          _recordImpression(activity, 'agent'),
                                      onAction: (action) =>
                                          unawaited(_openAgentAction(action)),
                                      onSaveSocialIntent: (task) => unawaited(
                                        _openIntentDraft(task: task),
                                      ),
                                      onSuggestion: (suggestion) => _composerKey
                                          .currentState
                                          ?.fillDraft(suggestion),
                                      onRetry: () =>
                                          unawaited(_workspace.retry()),
                                      onChooseCity: () =>
                                          unawaited(_chooseCity()),
                                      embedded: true,
                                      selectionPreview:
                                          currentSelection.isNotEmpty &&
                                              (_workspace.presentedResult ==
                                                      null ||
                                                  PlaceFactPresentation.read(
                                                        _workspace,
                                                        _workspace
                                                            .presentedResult!,
                                                      )?.entityID !=
                                                      currentSelection
                                                          .single
                                                          .id) &&
                                              _workspace.sheetExtent ==
                                                  AgentSheetExtent.peek
                                          ? _selectionCard(
                                              currentSelection.single,
                                            )
                                          : null,
                                      onOpenMapAreaItem: (item) =>
                                          unawaited(_openTypedMapDetail(item)),
                                      availableHeight: remaining.maxHeight,
                                    );
                                  },
                                ),
                              ),
                              Offstage(
                                offstage: hasSheet || hasHumanConversation,
                                child: Column(
                                  children: [
                                    if (browse &&
                                        !editing &&
                                        layout.surfaceHeight -
                                                preferredComposerHeight -
                                                2 * layout.surfaceInset >=
                                            48)
                                      _NowExploreHandle(
                                        expanded: browse,
                                        onToggle: () => setState(
                                          () => _exploreExpanded =
                                              !_exploreExpanded,
                                        ),
                                      ),
                                    Expanded(
                                      child: _RetainedMapContext(
                                        visible:
                                            !hasCreation &&
                                            !hasSheet &&
                                            hasContent,
                                        child: SingleChildScrollView(
                                          key: const PageStorageKey(
                                            'now-native-context-scroll',
                                          ),
                                          child: Column(
                                            crossAxisAlignment:
                                                CrossAxisAlignment.start,
                                            children: [
                                              if (showSelection)
                                                _selectionCard(
                                                  currentSelection.single,
                                                ),
                                              if (showLayerRecovery)
                                                Padding(
                                                  key: const Key(
                                                    'now-map-source-recovery',
                                                  ),
                                                  padding: const EdgeInsets.all(
                                                    8,
                                                  ),
                                                  child: Column(
                                                    crossAxisAlignment:
                                                        CrossAxisAlignment
                                                            .start,
                                                    children: [
                                                      Text(_layers.error!),
                                                      TextButton.icon(
                                                        onPressed:
                                                            _layers.loading ||
                                                                _layers.bounds ==
                                                                    null
                                                            ? null
                                                            : () => unawaited(
                                                                _layers.load(
                                                                  _layers
                                                                      .cityID!,
                                                                  _layers
                                                                      .bounds!,
                                                                ),
                                                              ),
                                                        icon: const Icon(
                                                          Icons.refresh,
                                                        ),
                                                        label: const Text(
                                                          '更新地图点位',
                                                        ),
                                                      ),
                                                    ],
                                                  ),
                                                ),
                                              if (_exploreExpanded &&
                                                  _layers.authorID == null &&
                                                  _layers.cityID ==
                                                      widget
                                                          .city
                                                          .selectedCity
                                                          ?.id &&
                                                  _layers.publicView != null)
                                                NowPublicPlaceList(
                                                  items: _layers.items,
                                                  truncated: _layers
                                                      .publicView!
                                                      .truncated,
                                                  // This existing list is grouped by place;
                                                  // map MOMENT taps still open the exact object.
                                                  onOpen: (item) =>
                                                      _openPublicPlacePanel(
                                                        item,
                                                        openPlacePreview: true,
                                                      ),
                                                ),
                                            ],
                                          ),
                                        ),
                                      ),
                                    ),
                                  ],
                                ),
                              ),
                              if (hasHumanConversation)
                                _humanConversationPanel(_friendConversation!),
                              if (hasCreation) _creationPanel(),
                            ],
                          ),
                        ),
                        Padding(
                          padding: const EdgeInsets.symmetric(
                            horizontal: NowUiTokens.composerInsetHorizontal,
                          ),
                          child: ConstrainedBox(
                            constraints: BoxConstraints(
                              maxHeight: composerMaxHeight,
                            ),
                            child: AgentComposer(
                              key: _composerKey,
                              onPreferredHeightChanged: (height) {
                                if (mounted &&
                                    (_composerHeight - height).abs() > .5) {
                                  setState(() => _composerHeight = height);
                                }
                              },
                              workspace: _workspace,
                              creationLabel: hasCreation
                                  ? _creation!.label
                                  : null,
                              creationInputHint: hasCreation
                                  ? (_creationTitleEditing
                                        ? '输入新的${_creation!.label}名称'
                                        : _creation!.inputHint)
                                  : null,
                              onExitCreation: hasCreation
                                  ? _exitCreation
                                  : null,
                              maxDraftBytes: hasCreation ? 5000 : 240,
                              videoPreview: _creationVideoPreview(),
                              videoStatus: _videoMaterials?.current == true
                                  ? _videoMaterials?.error
                                  : null,
                              recipientName: _composerDrafts.suspended
                                  ? null
                                  : _composerDrafts.target.displayName,
                              draftIdentity: (
                                _composerDrafts.identity,
                                _seedEntryEpoch,
                              ),
                              draftValue: _composerDrafts.suspended
                                  ? TextEditingValue.empty
                                  : _composerDrafts.value,
                              onDraftChanged: (value) {
                                ++_humanDraftRevision;
                                if (hasCreation) _retireCreationApprovals();
                                _composerDrafts.update(value);
                              },
                              submitBusy:
                                  (hasCreation &&
                                      (_creationBusy || _creationUncertain)) ||
                                  _friendConversation?.sending == true ||
                                  _workspace.queryState ==
                                      AgentQueryState.loading,
                              humanMode:
                                  !_composerDrafts.suspended &&
                                  _composerDrafts.target.human,
                              imageMaterialRecovering:
                                  widget.auth.sessionRestoring ||
                                  (_friendConversation?.imageDraft.busy ??
                                      _imageDraft?.busy ??
                                      false),
                              imageMaterialStatus: widget.auth.sessionRestoring
                                  ? '正在恢复账号，请稍后选择图片'
                                  : _needsLogin
                                  ? '登录后可选择图片'
                                  : _organizations.active != null
                                  ? '切回个人身份后可选择图片'
                                  : _composerDrafts.target.human &&
                                        !widget.friendImageEnabled
                                  ? '当前环境未启用会话图片'
                                  : _friendConversation?.audioActive == true
                                  ? '请先停止或取消录音，再选择图片'
                                  : _friendConversation?.pending == true
                                  ? '原消息结果待核实，请先核实再选择图片'
                                  : _friendConversation?.loading == true
                                  ? '正在读取当前会话，请稍后选择图片'
                                  : _friendConversation?.error ??
                                        _imageDraft?.error,
                              onRefreshImageMaterial:
                                  _friendConversation != null &&
                                      _friendConversation!.current &&
                                      !_friendConversation!.pending &&
                                      !_friendConversation!.audioActive &&
                                      !_friendConversation!.loading &&
                                      !_friendConversation!.sending &&
                                      !_friendConversation!.historyAvailable
                                  ? () => unawaited(_friendConversation!.load())
                                  : null,
                              friendConversation: _friendConversation,
                              needsLogin: _needsLogin,
                              onSignIn: () => unawaited(_signInHere()),
                              onVideoMaterial: !_composerDrafts.target.human
                                  ? () => unawaited(_openVideoMaterials())
                                  : null,
                              onAnalyzeImage:
                                  !hasCreation &&
                                      !_composerDrafts.target.human &&
                                      _imageDraft?.current == true
                                  ? (query) =>
                                        unawaited(_openAgentVisual(query))
                                  : null,
                              onCreateMoment: !_composerDrafts.target.human
                                  ? () => unawaited(_beginCreation('moment'))
                                  : null,
                              onCreateActivity: !_composerDrafts.target.human
                                  ? () => unawaited(_beginCreation('activity'))
                                  : null,
                              onCreateJourney: !_composerDrafts.target.human
                                  ? () => unawaited(_beginCreation('journey'))
                                  : null,
                              onCreateThemeMap: !_composerDrafts.target.human
                                  ? () => unawaited(_beginCreation('theme_map'))
                                  : null,
                              onCreateRoute: !_composerDrafts.target.human
                                  ? () => unawaited(_openRouteDraft())
                                  : null,
                              onImportPlan: !_composerDrafts.target.human
                                  ? () => unawaited(
                                      _openRouteDraft(importPlan: true),
                                    )
                                  : null,
                              onDraftContext:
                                  _composerDrafts.envelope != null &&
                                      !_composerDrafts.suspended
                                  ? () => unawaited(_openDraftContext())
                                  : null,
                              onSavePlace:
                                  !_composerDrafts.target.human &&
                                      (_workspace.result?.projectionItems?.any(
                                            (item) =>
                                                item.bookmarkKind == 'place' &&
                                                item.detail != null &&
                                                !_saved.contains(
                                                  'place',
                                                  item.detail!.id,
                                                ),
                                          ) ??
                                          false)
                                  ? () => unawaited(_choosePlaceToSave())
                                  : null,
                              imageUpload: _composerDrafts.target.human
                                  ? null
                                  : _taskImages,
                              imageDraft: _composerDrafts.target.human
                                  ? null
                                  : _imageDraft,
                              onCaptureImage: _imageSourceAction(
                                MomentImageSource.camera,
                              ),
                              onImageFile: _imageSourceAction(
                                MomentImageSource.file,
                              ),
                              onDraftImage: _composerDrafts.target.human
                                  ? widget.friendImageEnabled &&
                                            _friendConversation?.current ==
                                                true &&
                                            !_needsLogin
                                        ? () => unawaited(
                                            _composerKey.currentState
                                                    ?.selectFriendImage() ??
                                                Future<void>.value(),
                                          )
                                        : null
                                  : _imageDraft?.canSelect == true
                                  ? () => unawaited(_openDraftImage())
                                  : null,
                              onTaskImage:
                                  !_composerDrafts.target.human &&
                                      _taskImages?.canOpen == true
                                  ? () => unawaited(_openTaskImages())
                                  : null,
                              privateMomentImageContext: () => (
                                widget.moments,
                                widget.moments.identityEpoch,
                                _personalSourceSerial,
                              ),
                              materialContext: () => (
                                widget.auth,
                                _currentSeedIdentity,
                                _seedEntryEpoch,
                                widget.seedClient,
                                widget.seedApiBaseUrl,
                                widget.city,
                                widget.city.selectedCity?.id,
                                _online,
                                _online.selected?.id,
                                _workspace.queryContextType,
                              ),
                              materialContextChanges: Listenable.merge([
                                _activeIntentChanges,
                                widget.city,
                                _online,
                              ]),
                              onSubmit: _submit,
                              onSearchArea: _searchThisArea,
                              hasSearchArea:
                                  _online.selected == null &&
                                  _mapState.searchAreaBounds != null,
                              onlineMode: _online.selected != null,
                              suggestions: const [],
                              embedded: true,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
              if (showSocial)
                Positioned(
                  key: const Key('now-social-edge-position'),
                  top: topControlsHeight + 4,
                  right: 8,
                  bottom: layout.bottom + layout.surfaceHeight + 8,
                  child: NowSocialEdge(
                    key: ValueKey((
                      'now-social',
                      widget.auth.accountID ??
                          widget.auth.rejectedSessionOwnerID,
                    )),
                    open: true,
                    initialSection: _socialSection,
                    initialAllConversations: _socialAllConversations,
                    onAllConversationsChanged: (value) =>
                        _socialAllConversations = value,
                    onUnreadMessagesChanged: (value) {
                      if (_socialMessageUnread != value && mounted) {
                        setState(() => _socialMessageUnread = value);
                      }
                    },
                    initialFollowedContacts: _socialFollowedContacts,
                    onSectionChanged: (value) => _socialSection = value,
                    onContactsFilterChanged: (value) {
                      if (_socialFollowedContacts != value) {
                        _socialSelectionSerial++;
                        _socialFollowedContacts = value;
                      }
                    },
                    source: _socialSource,
                    identityChanges: _socialChanges,
                    current: _socialCurrent,
                    accountID: _socialAccount,
                    workspaceID: _socialWorkspace,
                    onOpenAuthor: (id) => unawaited(_openContactProfile(id)),
                    onOpenFollowTarget: (item) =>
                        unawaited(_openFollowTarget(item)),
                    onOpenConversation: (id) {
                      unawaited(_openConversationByID(id));
                    },
                    onChooseConversation: _selectFriendConversation,
                    onOpenFriend: (friend) =>
                        unawaited(_openFriendConversation(friend)),
                    onClose: _closeSocial,
                    onCloseConversation: _exitFriendConversation,
                    notificationBody: _socialNotifications(),
                    notificationTitle: _socialRequestScope == 'friend'
                        ? '好友申请'
                        : '通知',
                    onSectionChanging: (section) {
                      if (!_leaveSocialConversation()) return false;
                      setState(() => _socialRequestScope = null);
                      return true;
                    },
                    onFindPeople: () => unawaited(_openContactAction('search')),
                    onScanCode: () => unawaited(_openContactAction('scan')),
                    onShowOwnCode: () => unawaited(_openContactAction('own')),
                    onOpenMessageRequests: () =>
                        _showSocialRequests('conversation'),
                    messageRequestsBody: _socialRequestScope == 'conversation'
                        ? _socialRequests('conversation')
                        : null,
                    onCloseMessageRequests: () =>
                        setState(() => _socialRequestScope = null),
                    onOpenManagement: () => unawaited(_openFriendManagement()),
                    onSignIn: () => unawaited(_signInHere()),
                    onUsePersonal: () => _organizations.select(null),
                  ),
                ),
              if (_publicPlacePanel != null && _publicPlacePreview)
                Positioned(
                  key: const Key('now-place-preview-position'),
                  left:
                      (!_publicPlaceStoriesExpanded &&
                                  _selectedMapAnchor.value.id ==
                                      _publicPlaceMapID &&
                                  _selectedMapAnchor.value.point != null
                              ? _selectedMapAnchor.value.point!.dx - 125
                              : 16.0)
                          .clamp(
                            16.0,
                            (constraints.maxWidth - 266).clamp(
                              16.0,
                              double.infinity,
                            ),
                          ),
                  top: !previewVisible
                      ? 0.0
                      : (_selectedMapAnchor.value.id == _publicPlaceMapID &&
                                    _selectedMapAnchor.value.point != null
                                ? _selectedMapAnchor.value.point!.dy -
                                      previewViewport -
                                      12
                                : chromeBottom + 16)
                            .clamp(
                              chromeBottom,
                              mapBottom - previewViewport > chromeBottom
                                  ? mapBottom - previewViewport
                                  : chromeBottom,
                            ),
                  right: 16,
                  bottom: previewVisible
                      ? layout.bottom + layout.surfaceHeight + 8
                      : 0.0,
                  child: _RetainedMapContext(
                    visible: previewVisible,
                    child: Align(
                      alignment: Alignment.topLeft,
                      child: SingleChildScrollView(child: _publicPlacePanel!),
                    ),
                  ),
                ),
              if (_publicPlacePanel != null && !_publicPlacePreview)
                Positioned(
                  top: _publicPlacePanelMapVisible
                      ? (visibleHeight - 200).clamp(
                          chromeBottom,
                          constraints.maxHeight,
                        )
                      : chromeBottom,
                  left: 0,
                  right: 0,
                  bottom: 0,
                  child: Padding(
                    padding: EdgeInsets.only(
                      bottom: layout.bottom + layout.surfaceHeight,
                    ),
                    child: FocusTraversalGroup(child: _publicPlacePanel!),
                  ),
                ),
              if (editing &&
                  (compactChrome || hasSheet) &&
                  visibleHeight >=
                      (compactChrome
                              ? media.viewPadding.top + NowUiTokens.space8
                              : normalChromeBottom) +
                          NowUiTokens.actionHitSize)
                Positioned(
                  top: compactChrome
                      ? media.viewPadding.top + NowUiTokens.space8
                      : normalChromeBottom,
                  left: 16,
                  child: SizedBox.square(
                    dimension: NowUiTokens.actionHitSize,
                    child: NowGlassSurface(
                      radius: NowUiTokens.actionHitSize / 2,
                      child: IconButton(
                        key: const Key('now-sheet-keyboard-restore'),
                        tooltip: '收起键盘',
                        onPressed: _pauseComposer,
                        icon: const Icon(
                          Icons.keyboard_hide_outlined,
                          size: 20,
                        ),
                      ),
                    ),
                  ),
                ),
            ],
          );
        },
      ),
    ),
  );
}

/// A hidden list keeps its real viewport and offset without consuming map space.
class _RetainedMapContext extends StatefulWidget {
  const _RetainedMapContext({required this.visible, required this.child});
  final bool visible;
  final Widget child;
  @override
  State<_RetainedMapContext> createState() => _RetainedMapContextState();
}

class _RetainedMapContextState extends State<_RetainedMapContext> {
  double _height = 0;
  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, limits) {
      if (widget.visible && limits.maxHeight.isFinite)
        _height = limits.maxHeight;
      return Offstage(
        offstage: !widget.visible,
        child: OverflowBox(
          alignment: Alignment.topLeft,
          minHeight: _height,
          maxHeight: _height,
          child: SizedBox(
            height: _height,
            width: limits.maxWidth,
            child: widget.child,
          ),
        ),
      );
    },
  );
}

class _NowMapAreaCommand extends StatelessWidget {
  const _NowMapAreaCommand({required this.loading, required this.onPressed});
  final bool loading;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) => ConstrainedBox(
    constraints: const BoxConstraints(minHeight: 48),
    child: Center(
      widthFactor: 1,
      heightFactor: 1,
      child: TextButton.icon(
        key: const Key('now-map-area-command'),
        style: TextButton.styleFrom(
          minimumSize: const Size(0, 38),
          padding: const EdgeInsets.symmetric(horizontal: 14),
          tapTargetSize: MaterialTapTargetSize.padded,
          backgroundColor: Theme.of(
            context,
          ).colorScheme.surface.withValues(alpha: .94),
          foregroundColor: Theme.of(context).colorScheme.onSurfaceVariant,
          textStyle: Theme.of(context).textTheme.labelLarge,
          shape: const StadiumBorder(),
        ),
        onPressed: loading ? null : onPressed,
        icon: loading
            ? const SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(strokeWidth: 1.5),
              )
            : const Icon(Icons.search, size: 18),
        label: Text(loading ? '正在搜索…' : '搜索此区域'),
      ),
    ),
  );
}

class _NowExploreHandle extends StatelessWidget {
  const _NowExploreHandle({required this.expanded, required this.onToggle});
  final bool expanded;
  final VoidCallback onToggle;

  @override
  Widget build(BuildContext context) => Semantics(
    label: expanded ? '收起地图内容' : '浏览地图内容',
    button: true,
    child: GestureDetector(
      key: const Key('now-explore-handle'),
      behavior: HitTestBehavior.opaque,
      onTap: onToggle,
      onVerticalDragEnd: (details) {
        final velocity = details.primaryVelocity ?? 0;
        if ((velocity < 0 && !expanded) || (velocity > 0 && expanded))
          onToggle();
      },
      child: SizedBox(
        height: 48,
        width: double.infinity,
        child: Center(
          child: Container(
            width: 32,
            height: 4,
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.outlineVariant,
              borderRadius: BorderRadius.circular(2),
            ),
          ),
        ),
      ),
    ),
  );
}

/// A temporary directory search for geographic destinations only.
/// Online scopes and personal declarations remain in their existing settings.
class _NowCityPicker extends StatefulWidget {
  const _NowCityPicker({
    required this.city,
    required this.identityChanges,
    required this.current,
    required this.reloadCities,
    required this.onClose,
  });
  final PublicCityController city;
  final Listenable identityChanges;
  final bool Function() current;
  final Future<void> Function() reloadCities;
  final ValueChanged<String?> onClose;
  @override
  State<_NowCityPicker> createState() => _NowCityPickerState();
}

class _NowCityPickerState extends State<_NowCityPicker> {
  final _search = TextEditingController();
  bool _refreshQueued = false;
  bool _retired = false;
  bool get _valid => mounted && !_retired && widget.current();
  @override
  void initState() {
    super.initState();
    widget.city.addListener(_changed);
    widget.identityChanges.addListener(_changed);
    _search.addListener(_changed);
  }

  void _changed() {
    if (!mounted) return;
    if (!widget.current()) _retired = true;
    if (SchedulerBinding.instance.schedulerPhase ==
        SchedulerPhase.persistentCallbacks) {
      if (_refreshQueued) return;
      _refreshQueued = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        _refreshQueued = false;
        if (mounted) setState(() {});
      });
    } else {
      setState(() {});
    }
  }

  @override
  void dispose() {
    widget.city.removeListener(_changed);
    widget.identityChanges.removeListener(_changed);
    _search.removeListener(_changed);
    _search.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final tokens = NowUiTokens.reviewedOf(context);
    final query = _search.text.trim().toLowerCase();
    final cities = widget.city.cities
        .where(
          (city) => '${city.name} ${city.region}'.toLowerCase().contains(query),
        )
        .toList();
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: ConstrainedBox(
        key: const Key('now-scope-picker'),
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * .7,
        ),
        child: SafeArea(
          top: false,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 8, 8),
                child: Row(
                  children: [
                    Expanded(child: Text('选择城市', style: tokens.sheetTitle)),
                    IconButton(
                      tooltip: '关闭城市选择',
                      visualDensity: VisualDensity.standard,
                      constraints: const BoxConstraints(
                        minWidth: 48,
                        minHeight: 48,
                      ),
                      onPressed: () {
                        if (mounted) widget.onClose(null);
                      },
                      icon: const Icon(Icons.close),
                    ),
                  ],
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: TextField(
                  key: const Key('now-city-search'),
                  controller: _search,
                  style: tokens.composer,
                  decoration: InputDecoration(
                    hintText: '搜索城市',
                    hintStyle: tokens.composer.copyWith(
                      color: tokens.metadata.color,
                    ),
                    prefixIcon: const Icon(Icons.search),
                  ),
                ),
              ),
              const SizedBox(height: 8),
              Flexible(
                fit: FlexFit.loose,
                child: ListView(
                  key: const Key('now-scope-options'),
                  shrinkWrap: true,
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                  children: [
                    if (!_valid)
                      Text('城市目录已变化，请关闭后重新选择。', style: tokens.body)
                    else ...[
                      if (widget.city.citiesLoading)
                        const LinearProgressIndicator(),
                      if (widget.city.cityError case final error?)
                        Text(error, style: tokens.body),
                      for (final city in cities)
                        ListTile(
                          key: Key('now-scope-city-${city.id}'),
                          contentPadding: EdgeInsets.zero,
                          minTileHeight: 48,
                          title: Text(city.name, style: tokens.chrome),
                          subtitle: city.region.isEmpty
                              ? null
                              : Text(city.region, style: tokens.listSubtitle),
                          trailing: city.id == widget.city.selectedCity?.id
                              ? const Icon(Icons.check)
                              : null,
                          onTap: () {
                            if (_valid) widget.onClose(city.id);
                          },
                        ),
                      if (!widget.city.citiesLoading && cities.isEmpty)
                        Padding(
                          padding: const EdgeInsets.symmetric(vertical: 16),
                          child: Text(
                            query.isEmpty
                                ? '当前城市目录暂无可用目的地。'
                                : '当前城市目录中没有匹配的城市。',
                            style: tokens.body,
                          ),
                        ),
                      if (widget.city.cities.isEmpty ||
                          widget.city.cityError != null)
                        TextButton.icon(
                          style: TextButton.styleFrom(
                            minimumSize: const Size(48, 48),
                            textStyle: tokens.action,
                          ),
                          onPressed: widget.city.citiesLoading
                              ? null
                              : () {
                                  if (_valid) unawaited(widget.reloadCities());
                                },
                          icon: const Icon(Icons.refresh),
                          label: const Text('重新读取城市'),
                        ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _NowOnlineDetail extends StatefulWidget {
  const _NowOnlineDetail({required this.response});
  final NowOnlineResponse response;
  @override
  State<_NowOnlineDetail> createState() => _NowOnlineDetailState();
}

class _NowOnlineDetailState extends State<_NowOnlineDetail> {
  Timer? _expiry;
  bool _expired = false;
  @override
  void initState() {
    super.initState();
    final remaining = widget.response.items.single.expiresAt.difference(
      widget.response.observedAt,
    );
    _expiry = Timer(remaining.isNegative ? Duration.zero : remaining, () {
      if (mounted) setState(() => _expired = true);
    });
  }

  @override
  void dispose() {
    _expiry?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final item = widget.response.items.single;
    return Scaffold(
      appBar: birdtieAppBar(context, title: const Text('公开线上意图')),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          if (_expired)
            const Text('此意图已到期，请返回重新查询。')
          else ...[
            Text(item.title, style: Theme.of(context).textTheme.headlineSmall),
            const SizedBox(height: 16),
            Text('线上 · ${widget.response.context.label}'),
            const SizedBox(height: 12),
            Text('有效至 ${item.expiresAt.toLocal()}'),
          ],
        ],
      ),
    );
  }
}
