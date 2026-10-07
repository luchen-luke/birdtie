import 'dart:ui';

/// Ephemeral OCR lines, never an identity, instruction or safety verdict.
class MomentImageTextLine {
  const MomentImageTextLine(this.text, this.region);
  final String text;
  final Rect region;
}

class MomentImageTextHint {
  const MomentImageTextHint(this.label, this.region);
  final String label;
  final Rect region;
}

/// Deliberately narrow hints. Other writing and non-text pixels remain unknown.
List<MomentImageTextHint> momentImageTextHints(
  List<MomentImageTextLine> lines,
) {
  if (lines.length > 64) throw const FormatException('文字结果超过检查范围');
  var total = 0;
  final out = <MomentImageTextHint>[];
  final email = RegExp(
    r'[A-Za-z0-9.!#$%&\x27*+/=?^_`{|}~-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+',
  );
  final phone = RegExp(r'(?:\+?[0-9][0-9 ()-]{5,24}[0-9])');
  for (final line in lines) {
    total += line.text.length;
    final r = line.region;
    if (line.text.length > 1024 ||
        total > 8192 ||
        ![r.left, r.top, r.right, r.bottom].every((n) => n.isFinite) ||
        r.left < 0 ||
        r.top < 0 ||
        r.right > 1 ||
        r.bottom > 1 ||
        r.isEmpty) {
      throw const FormatException('文字位置或长度无效');
    }
    final isEmail = email.hasMatch(line.text);
    final isPhone = phone.allMatches(line.text).any((m) {
      final digits = m.group(0)!.replaceAll(RegExp(r'[^0-9]'), '');
      return digits.length >= 7 && digits.length <= 15;
    });
    if (isEmail || isPhone) {
      out.add(
        MomentImageTextHint(
          isEmail && isPhone
              ? '可能的邮箱或电话号码'
              : isEmail
              ? '可能的邮箱'
              : '可能的电话号码',
          r,
        ),
      );
    }
    if (out.length == 24) break;
  }
  return List.unmodifiable(out);
}
