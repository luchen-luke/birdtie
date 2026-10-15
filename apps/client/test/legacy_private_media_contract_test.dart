import 'dart:convert';

import 'package:birdtie_client/src/content/private_moment_media_controller.dart';
import 'package:birdtie_client/src/content/private_moment_media_pending_store.dart';
import 'package:birdtie_client/src/workspace/human_causal_control.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

const _owner = '11111111-1111-4111-8111-111111111111';
const _moment = '22222222-2222-4222-8222-222222222222';
const _image = '33333333-3333-4333-8333-333333333333';
const _operation = '44444444-4444-4444-8444-444444444444';
const _largeInput = 12 * 1024 * 1024;
final _hash = List.filled(64, 'a').join();

Map<String, dynamic> _receipt() => {
  'id': _image,
  'operationId': _operation,
  'momentId': _moment,
  'ownerAccountId': _owner,
  'momentRevision': 1,
  'mimeType': 'image/jpeg',
  'byteSize': _largeInput,
  'inputSha256': _hash,
  'sha256': _hash,
  'pixelRisk': 'UNKNOWN',
  'purpose': privateMomentImagePurpose,
  'status': 'ready_private',
  'origin': privateMomentLegacyImportOrigin,
  'revision': 1,
  'previewExpiresAt': '2026-10-15T00:05:00Z',
  'retainUntil': '9999-12-31T00:00:00Z',
};

Map<String, dynamic> _deleteReference() => {
  'schema': 'private-image-pending-v1',
  'ownerId': _owner,
  'momentId': _moment,
  'operationId': _operation,
  'phase': 'delete',
  'momentRevision': 1,
  'mimeType': 'image/jpeg',
  'byteSize': _largeInput,
  'inputSha256': _hash,
  'pixelRisk': 'UNKNOWN',
  'assetId': _image,
  'assetRevision': 1,
  'derivativeSha256': _hash,
  'observedAt': '2026-10-15T00:00:00.000Z',
  'deadlineAt': '9999-12-31T00:00:00.000Z',
  'origin': privateMomentLegacyImportOrigin,
};

void main() {
  test('旧导入大图仅按精确来源读取和恢复删除，普通上传限制保持', () {
    final receipt = PrivateMomentImageReceipt.fromJson(_receipt());
    expect(receipt.legacyPrivateImport, isTrue);
    expect(receipt.byteSize, _largeInput);
    expect(
      PrivateMomentImageReceipt.fromJson({
        ..._receipt(),
        'status': 'deleted',
      }).legacyPrivateImport,
      isTrue,
    );
    final ordinaryReceipt = _receipt()..remove('origin');
    for (final invalid in [
      ordinaryReceipt,
      {..._receipt(), 'origin': 'UNVERIFIED_IMPORT'},
      {..._receipt(), 'status': 'preview', 'byteSize': 1},
      {..._receipt(), 'byteSize': 16 * 1024 * 1024 + 1},
    ]) {
      expect(
        () => PrivateMomentImageReceipt.fromJson(invalid),
        throwsFormatException,
      );
    }

    final pending = PendingPrivateImageOperation.fromJson(_deleteReference());
    expect(pending.origin, privateMomentLegacyImportOrigin);
    expect(pending.deadlineAt.year, 9999);
    expect(
      PendingPrivateImageOperation.fromJson(pending.toJson()).toJson(),
      pending.toJson(),
    );
    final ordinaryDelete = _deleteReference()..remove('origin');
    for (final invalid in [
      {...ordinaryDelete, 'deadlineAt': '2026-10-15T00:05:00.000Z'},
      {...ordinaryDelete, 'byteSize': 1},
      {..._deleteReference(), 'origin': 'UNVERIFIED_IMPORT'},
      {..._deleteReference(), 'byteSize': 16 * 1024 * 1024 + 1},
      {
        ..._deleteReference(),
        'phase': 'save',
        'byteSize': 1,
        'derivativeSha256': null,
        'deadlineAt': '2026-10-15T00:05:00.000Z',
      },
    ]) {
      expect(
        () => PendingPrivateImageOperation.fromJson(invalid),
        throwsFormatException,
      );
    }
    final ordinaryPreview = {
      ...ordinaryDelete,
      'phase': 'preview',
      'byteSize': 1,
      'assetId': null,
      'assetRevision': null,
      'derivativeSha256': null,
      'deadlineAt': '2026-10-15T00:05:00.000Z',
    };
    expect(
      PendingPrivateImageOperation.fromJson(ordinaryPreview).toJson(),
      ordinaryPreview,
    );
    expect(
      () => PendingPrivateImageOperation.fromJson({
        ...ordinaryPreview,
        'origin': privateMomentLegacyImportOrigin,
      }),
      throwsFormatException,
    );
  });

  test('仅指定409错误码映射本地配图提示，不透传服务端消息', () async {
    var code = 'legacy_moment_photo_reselection_required';
    const untrustedMessage = '<script>UNTRUSTED_SERVER_MESSAGE</script>';
    final client = MockClient((_) async {
      return http.Response.bytes(
        utf8.encode(
          jsonEncode({
            'error': {'code': code, 'message': untrustedMessage},
          }),
        ),
        409,
      );
    });
    addTearDown(client.close);
    final api = HumanCausalApi(
      client: client,
      baseUrl: 'https://legacy-media.fixture',
      authorizationHeader: () => 'Bearer synthetic',
    );
    Future<Object?> request() => api.json(
      'POST',
      '/v1/me/moment-controls/previews',
      current: () => true,
    );
    await expectLater(
      request(),
      throwsA(
        isA<HumanCausalFailure>()
            .having(
              (e) => e.legacyMomentPhotoReselectionRequired,
              'code',
              isTrue,
            )
            .having((e) => e.message, 'action', contains('重新选择'))
            .having(
              (e) => e.message,
              'server text',
              isNot(contains(untrustedMessage)),
            ),
      ),
    );
    code = 'unlisted_error';
    await expectLater(
      request(),
      throwsA(
        isA<HumanCausalFailure>()
            .having(
              (e) => e.legacyMomentPhotoReselectionRequired,
              'code',
              isFalse,
            )
            .having(
              (e) => e.message,
              'generic message',
              '当前资料、权限或具体版本已变化，请重新读取。',
            ),
      ),
    );
  });
}
