import 'dart:typed_data';

const momentImageMaxBytes = 10 * 1024 * 1024;
const momentImageMaxDerivativeBytes = 16 * 1024 * 1024;
const momentImageMaxDimension = 8192;
const momentImageMaxPixels = 16 * 1024 * 1024;

class MomentImageHeader {
  const MomentImageHeader(this.mime, this.width, this.height);
  final String mime;
  final int width, height;
  static MomentImageHeader inspect(
    Uint8List b, {
    bool storedDerivative = false,
  }) {
    final limit = storedDerivative
        ? momentImageMaxDerivativeBytes
        : momentImageMaxBytes;
    if (b.isEmpty || b.length > limit) {
      throw const FormatException('图片超过处理大小限制或为空');
    }
    final data = ByteData.sublistView(b);
    void bounds(int w, int h) {
      if (w < 1 ||
          h < 1 ||
          w > momentImageMaxDimension ||
          h > momentImageMaxDimension ||
          w * h > momentImageMaxPixels) {
        throw const FormatException('图片尺寸超过处理范围');
      }
    }

    if (b.length >= 33 &&
        b[0] == 137 &&
        b[1] == 80 &&
        b[2] == 78 &&
        b[3] == 71 &&
        b[4] == 13 &&
        b[5] == 10 &&
        b[6] == 26 &&
        b[7] == 10) {
      if (data.getUint32(8) != 13 ||
          String.fromCharCodes(b.sublist(12, 16)) != 'IHDR') {
        throw const FormatException('PNG 图片头无效');
      }
      final w = data.getUint32(16), h = data.getUint32(20);
      bounds(w, h);
      var pos = 8;
      var ended = false;
      var heads = 0;
      var hasPixels = false;
      while (pos + 12 <= b.length) {
        final len = data.getUint32(pos);
        if (len > b.length - pos - 12) throw const FormatException('PNG 内容不完整');
        final type = String.fromCharCodes(b.sublist(pos + 4, pos + 8));
        if (type == 'IHDR' && (++heads != 1 || pos != 8)) {
          throw const FormatException('PNG 包含多个或迟到的图片头');
        }
        if (type == 'IDAT') hasPixels = true;
        if (['acTL', 'fcTL', 'fdAT'].contains(type)) {
          throw const FormatException('仅支持静态 PNG');
        }
        pos += len + 12;
        if (type == 'IEND') {
          ended = true;
          break;
        }
      }
      if (!ended || pos != b.length || !hasPixels) {
        throw const FormatException('PNG 内容不完整');
      }
      return MomentImageHeader('image/png', w, h);
    }
    if (b.length >= 4 && b[0] == 255 && b[1] == 216) {
      var pos = 2;
      MomentImageHeader? frame;
      var hasScan = false, ended = false;
      while (pos + 4 <= b.length) {
        if (b[pos++] != 255) break;
        while (pos < b.length && b[pos] == 255) {
          pos++;
        }
        if (pos >= b.length) break;
        final marker = b[pos++];
        if (marker == 217) {
          ended = true;
          break;
        }
        if (marker == 1 || (marker >= 208 && marker <= 215)) continue;
        if (pos + 2 > b.length) break;
        final len = data.getUint16(pos);
        if (len < 2 || len > b.length - pos) break;
        final isFrame =
            marker >= 192 && marker <= 207 && ![196, 200, 204].contains(marker);
        if (isFrame) {
          if (frame != null || ![192, 193, 194].contains(marker) || len < 8) {
            throw const FormatException('仅支持单帧 JPEG');
          }
          final components = b[pos + 7];
          if (components < 1 || components > 4 || len != 8 + 3 * components) {
            throw const FormatException('JPEG 图片头无效');
          }
          final h = data.getUint16(pos + 3), w = data.getUint16(pos + 5);
          bounds(w, h);
          frame = MomentImageHeader('image/jpeg', w, h);
        }
        pos += len;
        if (marker == 218) {
          if (frame == null) throw const FormatException('JPEG 缺少图片头');
          hasScan = true;
          // Traverse entropy bytes (including stuffing/restart markers) until
          // the next real segment. Progressive scans may not introduce a frame.
          while (pos < b.length) {
            if (b[pos] != 255) {
              pos++;
              continue;
            }
            final start = pos;
            while (pos < b.length && b[pos] == 255) {
              pos++;
            }
            if (pos >= b.length) break;
            final next = b[pos];
            if (next == 0 || (next >= 208 && next <= 215)) {
              pos++;
              continue;
            }
            pos = start;
            break;
          }
        }
      }
      // EOI itself has only two bytes, so it may be reached outside the loop.
      if (!ended && pos + 2 == b.length && b[pos] == 255 && b[pos + 1] == 217) {
        pos += 2;
        ended = true;
      }
      if (ended && pos == b.length && frame != null && hasScan) return frame;
      throw const FormatException('JPEG 图片头无效');
    }
    throw const FormatException('仅支持静态 JPEG 或 PNG 图片');
  }
}
