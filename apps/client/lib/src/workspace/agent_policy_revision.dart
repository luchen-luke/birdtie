import 'package:flutter/foundation.dart' show kIsWeb;

/// Checks the current revision while reserving one exact increment.
///
/// [isWeb] is a code-level test seam. Production callers use [kIsWeb]; the
/// response payload cannot choose its numeric representation or relax a bound.
bool agentPolicyRevisionValid(Object? value, {bool isWeb = kIsWeb}) {
  if (value is! int || value < 0) return false;
  // JS safe integers end at 2^53 - 1. Keep current + 1 inside that domain.
  if (isWeb) return value <= 9007199254740990;
  // Preserve the original native int64 maximum-minus-one contract without an
  // integer literal that the JS compiler cannot represent exactly. This branch
  // is never evaluated by production Web callers.
  return value <= int.parse('9223372036854775806');
}
