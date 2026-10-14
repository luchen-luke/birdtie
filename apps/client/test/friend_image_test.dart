import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:birdtie_client/src/workspace/chat_message_operation.dart';
import 'package:birdtie_client/src/workspace/friend_image.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'private_moment_media_controller_test.dart' show syntheticPrivatePNG;

const imageOwner = '11111111-1111-4111-8111-111111111111';
const imageConversation = '22222222-2222-4222-8222-222222222222';
const imagePeer = '33333333-3333-4333-8333-333333333333';
const imageOperation = '44444444-4444-4444-8444-444444444444';
const imageMessage = '55555555-5555-4555-8555-555555555555';

Future<Uint8List> imageFixtureBytes() => syntheticPrivatePNG();
FriendImageMetadata imageMetadata(Uint8List bytes) {
  final derivative = FriendImageDerivative(mimeType: 'image/png',
      byteSize: bytes.length, sha256: sha256.convert(bytes).toString(), width: 4, height: 4);
  return FriendImageMetadata(full: derivative, thumbnail: derivative);
}
PendingFriendImage imagePending(FriendImageDraft draft, String caption, {
  String conversation = imageConversation, String peer = imagePeer,
}) => PendingFriendImage.review(operationID: imageOperation,
    conversationID: conversation, recipientID: peer, draft: draft, caption: caption);
Map<String, Object> imageReceipt(PendingFriendImage pending, FriendImageMetadata image, {
  String owner = imageOwner,
}) => {
  'schemaVersion': friendImageReceiptSchema, 'ownerId': owner,
  'conversationId': pending.conversationID, 'recipientAccountId': pending.recipientID,
  'operationId': pending.operationID,
  'effectKey': humanMessageEffectKey(owner, pending.operationID),
  'payloadDigest': pending.payloadDigest, 'messageId': imageMessage,
  'inputMimeType': pending.inputMimeType, 'inputSize': pending.inputSize,
  'inputSha256': pending.inputSha256, 'pixelRisk': pending.pixelRisk,
  'captionSha256': pending.captionSha256, 'purpose': friendImagePurpose,
  'createdAt': '2026-10-12T01:02:03Z', 'image': image.toJson(),
};
http.Response imageWire(Object data, {int status = 200}) => http.Response.bytes(
    utf8.encode(jsonEncode({'data': data})), status,
    headers: {'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store'});

class _HeadersClient extends http.BaseClient {
  _HeadersClient(this.reply);
  final Future<http.StreamedResponse> Function(http.BaseRequest request) reply;
  int closes = 0;
  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) => reply(request);
  @override
  void close() { closes++; }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('UTF8 framed digest matches the API candidate vector, without image decoding', () {
    expect(friendImagePayloadDigest(conversation: imageConversation, recipient: imagePeer,
        mimeType: 'image/png', inputSize: 3,
        inputSha256: '039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81',
        pixelRisk: 'USER_MASKED', caption: '你好\ncafé'),
        'd9ef834761e8b2bb0b2ad74c726e3131e79d96ba975b5d546ac0c35578b217da');
    expect(humanMessageEffectKey(imageOwner, imageOperation),
        '5bc69c34eb8ac769f76d90b7ac044f7c428af2c06db0d30ebcb9bcf47ee22fe1');
  });

  test('caption uses exact UTF8, permits image-only, rejects NUL and >2000 bytes', () {
    expect(friendImageCaption(''), isTrue);
    expect(friendImageCaption(List.filled(1000, 'é').join()), isTrue);
    expect(friendImageCaption(List.filled(1001, 'é').join()), isFalse);
    expect(friendImageCaption('a\u0000b'), isFalse);
    expect(friendImageCaption(String.fromCharCode(0xd800)), isFalse);
  });

  test('closed image JSON rejects duplicate escaped keys and unknown keys', () async {
    expect(() => decodeFriendImageJson('{"image":1,"im\\u0061ge":2}'), throwsFormatException);
    final meta = imageMetadata(await imageFixtureBytes()).toJson();
    expect(() => FriendImageMetadata.decode({...meta, 'url': 'https://unit.example/foreign'}),
        throwsFormatException);
    final thumbnail = Map<String, Object>.from(meta['thumbnail']! as Map);
    expect(() => FriendImageMetadata.decode({...meta, 'thumbnail': {...thumbnail, 'width': 3}}),
        throwsFormatException);
  });

  test('receipt binds original input and accepts independent final derivative SHA', () async {
    final bytes = await imageFixtureBytes(), meta = imageMetadata(bytes);
    final draft = FriendImageDraft(bytes, width: 4, height: 4);
    final pending = imagePending(draft, ' exact caption ');
    final derivative = {...meta.toJson(), 'sha256': List.filled(64, 'a').join()};
    final receipt = imageReceipt(pending, FriendImageMetadata.decode(derivative));
    expect(FriendImageReceipt.decode(receipt, imageOwner, pending).image.full.sha256,
        isNot(draft.inputSha256));
    for (final key in ['ownerId', 'conversationId', 'recipientAccountId', 'operationId',
      'payloadDigest', 'inputSha256', 'captionSha256', 'purpose']) {
      expect(() => FriendImageReceipt.decode({...receipt, key: 'wrong'}, imageOwner, pending),
          throwsFormatException, reason: key);
    }
    expect(receipt.length, 16);
    expect(() => FriendImageReceipt.decode({...receipt, 'createdAt': '2026-02-31T01:02:03Z'},
        imageOwner, pending), throwsFormatException);
  });

  test('journal contains metadata only and refuses conflicting same operation', () async {
    final draft = FriendImageDraft(await imageFixtureBytes(), width: 4, height: 4);
    final pending = imagePending(draft, 'private caption');
    final store = MemoryFriendImagePendingStore();
    await store.write('https://unit.example', imageOwner, pending);
    expect(pending.toJson().keys, isNot(contains('contentBase64')));
    expect(pending.toJson().keys, isNot(contains('caption')));
    expect(await store.read('https://other.example', imageOwner), isEmpty);
    expect(await store.read('https://unit.example', imagePeer), isEmpty);
    await expectLater(store.write('https://unit.example', imageOwner,
        imagePending(draft, 'edited caption')), throwsFormatException);
  });

  test('disabled capability makes zero requests and never closes shared transport', () async {
    var requests = 0;
    final client = _HeadersClient((_) async {
      requests++; throw StateError('disabled route must not execute');
    });
    final source = FriendImageSource(client: client, environment: 'https://unit.example',
        authorizationHeader: () => 'Bearer synthetic', current: () => true, binding: () => 0);
    final pending = imagePending(FriendImageDraft(await imageFixtureBytes(), width: 4, height: 4), '');
    await expectLater(source.recover(imageOwner, pending), throwsStateError);
    source.dispose();
    expect(requests, 0);
    expect(client.closes, 0);
    client.close();
  });

  test('POST has nine keys, exact PNG/caption, recipient and original operation', () async {
    final bytes = await imageFixtureBytes(), meta = imageMetadata(bytes);
    final draft = FriendImageDraft(bytes, width: 4, height: 4);
    final pending = imagePending(draft, ' 你好\n ');
    final client = MockClient((request) async {
      expect(request.method, 'POST');
      expect(request.url.path, '/v1/me/conversations/$imageConversation/image-message-operations');
      final input = jsonDecode(request.body) as Map<String, dynamic>;
      expect(input.keys.toSet(), {'operationId', 'recipientAccountId', 'purpose', 'caption',
        'pixelRisk', 'mimeType', 'inputSize', 'inputSha256', 'contentBase64'});
      expect(input['caption'], ' 你好\n ');
      expect(input['recipientAccountId'], imagePeer);
      expect(base64Decode(input['contentBase64']), bytes);
      return imageWire(imageReceipt(pending, meta), status: 201);
    });
    final source = FriendImageSource(client: client, environment: 'https://unit.example',
        authorizationHeader: () => 'Bearer synthetic', current: () => true,
        binding: () => 0, enabled: true);
    addTearDown(source.dispose); addTearDown(client.close);
    expect((await source.send(imageOwner, pending, draft, ' 你好\n ')).messageID, imageMessage);
  });

  test('retired success headers cancel before copying a response body', () async {
    final headers = Completer<http.StreamedResponse>();
    var epoch = 0, delivered = 0, cancelled = 0;
    final body = StreamController<List<int>>(onCancel: () { cancelled++; });
    final client = _HeadersClient((_) => headers.future);
    final source = FriendImageSource(client: client, environment: 'https://unit.example',
        authorizationHeader: () => 'Bearer synthetic', current: () => true,
        binding: () => epoch, enabled: true);
    final pending = imagePending(FriendImageDraft(await imageFixtureBytes(), width: 4, height: 4), '');
    final read = source.recover(imageOwner, pending);
    final assertion = expectLater(read, throwsStateError);
    epoch++;
    headers.complete(http.StreamedResponse(body.stream.map((chunk) { delivered++; return chunk; }),
        200, headers: {'content-type': 'application/json', 'cache-control': 'no-store'}));
    await assertion;
    expect(delivered, 0); expect(cancelled, 1);
    source.dispose(); client.close(); await body.close();
  });

  test('content checks variant and metadata headers before body subscription', () async {
    final bytes = await imageFixtureBytes();
    var delivered = 0;
    final client = _HeadersClient((_) async => http.StreamedResponse(
        Stream.value(bytes).map((chunk) { delivered++; return chunk; }), 200,
        headers: {'content-type': 'image/png', 'content-length': bytes.length.toString(),
          'x-content-sha256': sha256.convert(bytes).toString(),
          'x-image-width': '4', 'x-image-height': '4', 'x-image-variant': 'full',
          'cache-control': 'no-store', 'x-content-type-options': 'nosniff'}));
    final source = FriendImageSource(client: client, environment: 'https://unit.example',
        authorizationHeader: () => 'Bearer synthetic', current: () => true,
        binding: () => 0, enabled: true);
    await expectLater(source.content(conversation: imageConversation, message: imageMessage,
        metadata: imageMetadata(bytes), variant: FriendImageVariant.thumbnail), throwsFormatException);
    expect(delivered, 0);
    source.dispose(); client.close();
  });
}
