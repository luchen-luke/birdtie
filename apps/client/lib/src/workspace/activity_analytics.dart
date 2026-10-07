import 'dart:convert';

import 'package:http/http.dart' as http;
import '../config/birdtie_environment.dart';

/// Records a visible public activity without sending a person or location ID.
Future<bool> recordActivityView(
  String activityID,
  String eventType,
  String entrySource, {
  String? apiBaseUrl,
  http.Client? client,
}) async {
  const configuredBase = BirdtieEnvironment.apiBaseUrl;
  final base = apiBaseUrl ?? configuredBase;
  if (base.isEmpty) return false;
  final ownedClient = client == null;
  final caller = client ?? http.Client();
  try {
    final response = await caller
        .post(
          Uri.parse(
            '${base.replaceFirst(RegExp(r'/$'), '')}/v1/activities/${Uri.encodeComponent(activityID)}/analytics/events',
          ),
          headers: const {'Content-Type': 'application/json'},
          body: jsonEncode({
            'eventType': eventType,
            'entrySource': entrySource,
          }),
        )
        .timeout(const Duration(seconds: 5));
    return response.statusCode == 204;
  } catch (_) {
    return false;
  } finally {
    if (ownedClient) caller.close();
  }
}
