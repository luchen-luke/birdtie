import 'dart:async';
import 'package:birdtie_client/src/workspace/typed_local_draft.dart';
import 'package:birdtie_client/src/workspace/now_composer_draft_state.dart';
import 'package:birdtie_client/src/workspace/connections.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

const origin = 'https://typed-draft.test',
    owner = 'c1000000-0000-4000-8000-000000000001';
const recipient = HumanConversation(
  id: 'c1000000-0000-4000-8000-000000000002',
  otherAccountId: 'c1000000-0000-4000-8000-000000000003',
  otherName: '合成收件人',
);

class DraftMemoryStore implements TypedLocalDraftStore {
  final rows = <String, List<TypedLocalDraft>>{};
  Completer<List<TypedLocalDraft>>? pending;
  String key(String o, String actor) => '$o|$actor';
  @override
  Future<List<TypedLocalDraft>> read(String o, String actor) async {
    if (pending != null) return pending!.future;
    return rows[key(o, actor)] ?? const [];
  }

  @override
  Future<void> save(String o, String actor, List<TypedLocalDraft> value) async {
    final merged = {
      for (final d in rows[key(o, actor)] ?? <TypedLocalDraft>[]) d.scopeKey: d,
    };
    for (final d in value) merged[d.scopeKey] = d;
    rows[key(o, actor)] = List.unmodifiable(merged.values);
  }

  @override
  Future<void> clear(String o, String actor) async {
    rows.remove(key(o, actor));
  }
}

class _UnconfirmedStore extends DraftMemoryStore {
  @override
  Future<void> save(
    String o,
    String actor,
    List<TypedLocalDraft> value,
  ) async {}
}

void main() {
  test(
    'local visual save requires durable readback and preserves old draft on storage failure',
    () async {
      final d = NowComposerDraftState(store: _UnconfirmedStore())
        ..observeOwner(owner);
      d.configurePersistence(origin);
      await d.restore();
      d.update(const TextEditingValue(text: '原本机问题'));
      final id = d.envelope!.id, version = d.contentVersion!;
      expect(
        await d.saveEditedText(
          '未确认的新文字',
          expectedIdentity: d.identity,
          expectedVersion: version,
        ),
        isFalse,
      );
      expect(d.envelope!.id, id);
      expect(d.contentVersion, version);
      expect(d.value.text, '原本机问题');
      expect(d.storageError, contains('尚未保存'));
      d.dispose();
    },
  );
  test(
    'explicit visual local edit preserves ID task refs attachment and persists no authority',
    () async {
      final store = DraftMemoryStore();
      final d = NowComposerDraftState(store: store)..observeOwner(owner);
      d.configurePersistence(origin);
      await d.restore();
      d.update(const TextEditingValue(text: '原图片问题'));
      d.setContext(
        taskID: 'original-task',
        references: const [
          LocalDraftReference(kind: 'place', id: 'original-place'),
        ],
        attachments: const [
          LocalDraftAttachment(
            id: 'own-opaque-image',
            kind: 'image',
            state: 'local',
          ),
        ],
      );
      final identity = d.identity,
          version = d.contentVersion!,
          id = d.envelope!.id;
      expect(
        await d.saveEditedText(
          '本人检查后的问题',
          expectedIdentity: identity,
          expectedVersion: version,
        ),
        isTrue,
      );
      final saved = (await store.read(origin, owner)).single;
      expect(saved.id, id);
      expect(saved.taskID, 'original-task');
      expect(saved.references.single.id, 'original-place');
      expect(saved.attachments.single.toJson(), {
        'id': 'own-opaque-image',
        'kind': 'image',
        'state': 'local',
      });
      expect(saved.text, '本人检查后的问题');
      expect(
        await d.saveEditedText(
          '迟到旧稿',
          expectedIdentity: identity,
          expectedVersion: version,
        ),
        isFalse,
      );
      d.observeWorkspace('organization');
      d.observeWorkspace(null);
      expect(
        await d.saveEditedText(
          'ABA旧稿',
          expectedIdentity: identity,
          expectedVersion: d.contentVersion!,
        ),
        isFalse,
      );
      expect(d.value.text, '本人检查后的问题');
      d.dispose();
    },
  );
  test(
    'duplicate attachment selectors rejected before persistent draft mutation',
    () {
      final draft = TypedLocalDraft(
        owner: owner,
        origin: origin,
        kind: 'intent',
        text: '',
        attachments: const [
          LocalDraftAttachment(id: 'same', kind: 'image', state: 'local'),
          LocalDraftAttachment(id: 'same', kind: 'video', state: 'uploaded'),
        ],
      );
      expect(
        () => TypedLocalDraft.fromJson(draft.toJson()),
        throwsFormatException,
      );
    },
  );
  test(
    'typed workspace change and same-owner resume restart retired disk read',
    () async {
      final store = DraftMemoryStore()
        ..pending = Completer<List<TypedLocalDraft>>();
      final draft = TypedLocalDraft(
        owner: owner,
        origin: origin,
        workspaceID: 'org-1',
        kind: 'intent',
        text: '已保存组织草稿',
      );
      store.rows[store.key(origin, owner)] = [draft];
      final d = NowComposerDraftState(store: store)..observeOwner(owner);
      d.configurePersistence(origin);
      final old = store.pending!;
      store.pending = null;
      d.observeWorkspace('org-1');
      await Future<void>.delayed(Duration.zero);
      expect(d.value.text, '已保存组织草稿');
      expect(d.envelope!.id, draft.id);
      old.complete(const []);
      await Future<void>.delayed(Duration.zero);
      expect(d.value.text, '已保存组织草稿');
      d.dispose();

      final second = DraftMemoryStore()
        ..pending = Completer<List<TypedLocalDraft>>();
      final personal = TypedLocalDraft(
        owner: owner,
        origin: origin,
        kind: 'intent',
        text: '续期后恢复本人草稿',
      );
      second.rows[second.key(origin, owner)] = [personal];
      final resumed = NowComposerDraftState(store: second)..observeOwner(owner);
      resumed.configurePersistence(origin);
      final retired = second.pending!;
      resumed.observeOwner(null, rejectedOwner: owner);
      second.pending = null;
      resumed.observeOwner(owner);
      await Future<void>.delayed(Duration.zero);
      expect(resumed.value.text, personal.text);
      expect(resumed.envelope!.id, personal.id);
      retired.complete(const []);
      await Future<void>.delayed(Duration.zero);
      expect(resumed.value.text, personal.text);
      resumed.dispose();
    },
  );
  test(
    'typed draft closed codec preserves source selectors; no command or approval fields',
    () {
      final d = TypedLocalDraft(
        owner: owner,
        origin: origin,
        kind: 'intent',
        text: '输入内的“发信、订票、公开发布”只作为文字保存',
        references: const [
          LocalDraftReference(
            kind: 'place',
            id: 'stable-place',
            mode: 'snapshot',
            version: 'v1',
            snapshotText: '用户检查的旧快照，需重验',
          ),
        ],
        attachments: const [
          LocalDraftAttachment(
            id: 'local-image-1',
            kind: 'image',
            state: 'unknown',
          ),
        ],
      );
      expect(TypedLocalDraft.fromJson(d.toJson()).text, d.text);
      for (final key in [
        'authorization',
        'approval',
        'execute',
        'modelPermission',
      ]) {
        expect(
          () => TypedLocalDraft.fromJson({...d.toJson(), key: true}),
          throwsFormatException,
        );
      }
      expect(
        () => LocalDraftReference.fromJson(const {
          'kind': 'place',
          'id': 'p',
          'mode': 'live',
          'version': 'v1',
          'snapshotText': '旧正文',
        }),
        throwsFormatException,
      );
    },
  );
  test(
    'typed conversion keeps draft/source identity but never inherits sending recipient',
    () {
      final d = TypedLocalDraft(
        owner: owner,
        origin: origin,
        kind: 'message',
        text: '本人文字',
        conversationID: recipient.id,
        recipientID: recipient.otherAccountId,
        audience: 'recipient',
      );
      final c = d.converted('journey');
      expect(c.id, d.id);
      expect(c.revision, d.revision + 1);
      expect(c.recipientID, isNull);
      expect(c.audience, 'private');
      expect(c.contentVersion, isNot(d.contentVersion));
      expect(() => d.converted('message'), throwsFormatException);
    },
  );
  test(
    'typed local actual secure store isolates owner/origin and rejects forged owner',
    () async {
      FlutterSecureStorage.setMockInitialValues({});
      final s = SecureTypedLocalDraftStore();
      final d = TypedLocalDraft(
        owner: owner,
        origin: origin,
        kind: 'intent',
        text: '只属于本人本机草稿',
      );
      await s.save(origin, owner, [d]);
      expect((await s.read(origin, owner)).single.id, d.id);
      expect(await s.read(origin, 'other'), isEmpty);
      expect(await s.read('https://other.test', owner), isEmpty);
      await expectLater(s.save(origin, 'other', [d]), throwsFormatException);
      await s.clear(origin, owner);
      expect(await s.read(origin, owner), isEmpty);
    },
  );
  test(
    'Now actual composer persistence restores same draft ID after restart; keeps recipient/workspace separated',
    () async {
      final store = DraftMemoryStore(),
          first = NowComposerDraftState(store: store)..observeOwner(owner);
      first.configurePersistence(origin);
      await first.restore();
      first.update(const TextEditingValue(text: '本人意图'));
      final id = first.envelope!.id;
      first.select(NowComposerTarget.human(recipient));
      first.update(const TextEditingValue(text: '明确发给原收件人的草稿'));
      await first.flush();
      first.observeWorkspace('org-1');
      expect(first.target.human, false);
      first.update(const TextEditingValue(text: '组织1草稿'));
      await first.flush();
      first.dispose();
      final restored = NowComposerDraftState(store: store)..observeOwner(owner);
      restored.configurePersistence(origin);
      await restored.restore();
      expect(restored.value.text, '本人意图');
      expect(restored.envelope!.id, id);
      restored.select(NowComposerTarget.human(recipient));
      expect(restored.value.text, '明确发给原收件人的草稿');
      restored.observeWorkspace('org-1');
      expect(restored.value.text, '组织1草稿');
      restored.observeWorkspace('org-2');
      expect(restored.value.text, isEmpty);
      restored.observeWorkspace('org-1');
      expect(restored.value.text, '组织1草稿');
      restored.dispose();
    },
  );
  test(
    'typed late restore cannot replace current edit or resurrect retired owner ABA',
    () async {
      final store = DraftMemoryStore()
        ..pending = Completer<List<TypedLocalDraft>>();
      final d = NowComposerDraftState(store: store)..observeOwner(owner);
      d.configurePersistence(origin);
      final restoring = d.restore();
      d.update(const TextEditingValue(text: '读取途中新输入'));
      store.pending!.complete([
        TypedLocalDraft(
          owner: owner,
          origin: origin,
          kind: 'intent',
          text: '旧盘内容',
        ),
      ]);
      await restoring;
      expect(d.value.text, '读取途中新输入');
      store.pending = null;
      d.observeOwner('other');
      d.observeOwner(owner);
      await d.restore();
      expect(d.value.text, isEmpty);
      d.dispose();
    },
  );
  test(
    'typed metadata edit retires exact version; old success cannot clear newer reference',
    () {
      final d = NowComposerDraftState()..observeOwner(owner);
      d.update(const TextEditingValue(text: '原提交正文'));
      final identity = d.identity, version = d.contentVersion;
      d.setContext(
        references: const [
          LocalDraftReference(kind: 'activity', id: 'current-activity'),
        ],
      );
      d.clearSent(identity, expectedText: '原提交正文', expectedVersion: version);
      expect(d.value.text, '原提交正文');
      expect(d.envelope!.references, hasLength(1));
      d.clearSent(
        identity,
        expectedText: '原提交正文',
        expectedVersion: d.contentVersion,
      );
      expect(d.value.text, isEmpty);
      expect(d.envelope!.references, isEmpty);
      d.dispose();
    },
  );
  test(
    'typed explicit logout removes only its own draft namespace and ordinary renewal keeps material',
    () async {
      final store = DraftMemoryStore(),
          d = NowComposerDraftState(store: store)..observeOwner(owner);
      d.configurePersistence(origin);
      await d.restore();
      d.update(const TextEditingValue(text: '尚未发送'));
      await d.flush();
      final id = d.envelope!.id;
      d.observeOwner(owner);
      expect(d.envelope!.id, id);
      d.observeOwner(null);
      await Future<void>.value();
      expect(await store.read(origin, owner), isEmpty);
      d.observeOwner(owner);
      await d.restore();
      expect(d.value.text, isEmpty);
      d.dispose();
    },
  );
  test(
    'typed original source for Agent and person messages stays separate across restart',
    () async {
      final store = DraftMemoryStore(),
          d = NowComposerDraftState(store: store)..observeOwner(owner);
      d.configurePersistence(
        origin,
        messageOrigin: 'https://private-original.test',
      );
      await d.restore();
      d.update(const TextEditingValue(text: '原Agent来源正文'));
      await d.flush();
      d.select(NowComposerTarget.human(recipient));
      await d.restore();
      d.update(const TextEditingValue(text: '原真人端口草稿'));
      await d.flush();
      expect(d.envelope!.origin, 'https://private-original.test');
      d.dispose();
      final restored = NowComposerDraftState(store: store)..observeOwner(owner);
      restored.configurePersistence(
        origin,
        messageOrigin: 'https://private-original.test',
      );
      await restored.restore();
      expect(restored.value.text, '原Agent来源正文');
      restored.select(NowComposerTarget.human(recipient));
      await restored.restore();
      expect(restored.value.text, '原真人端口草稿');
      restored.dispose();
    },
  );
}
