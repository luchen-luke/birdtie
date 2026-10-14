/// A revocable person selector; never an authorization or login credential.
final _contactAccountID = RegExp(
  r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$',
);

bool validContactAccountID(String value) =>
    _contactAccountID.hasMatch(value) &&
    value.toLowerCase() != '00000000-0000-0000-0000-000000000000';

String contactCodeFor(String accountID) {
  if (!validContactAccountID(accountID)) {
    throw const FormatException('个人标识不可用');
  }
  return 'birdtie://contact/${accountID.toLowerCase()}';
}

String contactAccountFromCode(String raw) {
  // Reject unknown parameters, credentials, fragments and encoded redirects.
  // The ID must be resolved by the service; it is not a public account ID.
  const prefix = 'birdtie://contact/';
  if (!raw.startsWith(prefix) || raw.length != prefix.length + 36) {
    throw const FormatException('这不是 Birdtie 联系人二维码，请重新扫描。');
  }
  final accountID = raw.substring(prefix.length);
  if (!validContactAccountID(accountID)) {
    throw const FormatException('这不是 Birdtie 联系人二维码，请重新扫描。');
  }
  return accountID.toLowerCase();
}

class ContactCodeView {
  ContactCodeView.fromJson(Map<String, dynamic> data)
    : version = data['version'] as int,
      codeID = data['codeId'] as String?,
      expiresAt = data['expiresAt'] == null
          ? null
          : DateTime.parse(data['expiresAt'] as String) {
    if (data.length != 3 ||
        !data.keys.toSet().containsAll({'version', 'codeId', 'expiresAt'}) ||
        version < 0 ||
        version > 4503599627370496 ||
        (codeID == null) != (expiresAt == null) ||
        codeID != null && !validContactAccountID(codeID!) ||
        expiresAt != null && !expiresAt!.isUtc) {
      throw const FormatException('联系人码结果不可读取');
    }
  }
  final int version;
  final String? codeID;
  final DateTime? expiresAt;
  bool activeAt(DateTime now) => codeID != null && expiresAt!.isAfter(now);
}
