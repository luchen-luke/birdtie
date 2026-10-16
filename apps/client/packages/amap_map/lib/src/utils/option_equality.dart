/// Equality for channel-serializable map options. Shared style byte buffers
/// take the identity fast path; changed values compare structurally without
/// allocating a multi-megabyte diagnostic string on the UI thread.
bool amapOptionValuesEqual(Object? previous, Object? next) {
  if (identical(previous, next)) return true;
  if (previous is Map && next is Map) {
    if (previous.length != next.length) return false;
    for (final Object? key in previous.keys) {
      if (!next.containsKey(key) ||
          !amapOptionValuesEqual(previous[key], next[key])) {
        return false;
      }
    }
    return true;
  }
  if (previous is List && next is List) {
    if (previous.length != next.length) return false;
    for (int i = 0; i < previous.length; i++) {
      if (!amapOptionValuesEqual(previous[i], next[i])) return false;
    }
    return true;
  }
  return previous == next;
}
