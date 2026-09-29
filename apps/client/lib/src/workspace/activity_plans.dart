import 'dart:async';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;

class ActivityPlan {
  const ActivityPlan({
    required this.id,
    required this.activityId,
    required this.title,
    required this.cityId,
    required this.status,
    required this.available,
    required this.startsAt,
  });

  final String id;
  final String activityId;
  final String title;
  final String cityId;
  final String status;
  final bool available;
  final DateTime? startsAt;

  factory ActivityPlan.fromJson(Map<String, dynamic> json) => ActivityPlan(
    id: json['id'] as String,
    activityId: json['activityId'] as String,
    title: json['title'] as String? ?? '',
    cityId: json['cityId'] as String? ?? '',
    status: json['status'] as String? ?? 'unavailable',
    available: json['available'] as bool? ?? false,
    startsAt: DateTime.tryParse(json['startsAt'] as String? ?? ''),
  );
}

class ActivityPlansController extends ChangeNotifier {
  ActivityPlansController({
    required this.authorizationHeader,
    http.Client? client,
    String? apiBaseUrl,
  }) : _client = client ?? http.Client(),
       _apiBaseUrl = apiBaseUrl ?? apiBase;

  static const apiBase = String.fromEnvironment('BIRDTIE_API_BASE_URL');
  final String? Function() authorizationHeader;
  final http.Client _client;
  final String _apiBaseUrl;
  bool get configured => _apiBaseUrl.isNotEmpty;
  List<ActivityPlan> plans = const [];
  bool loading = false;
  bool failed = false;
  final Set<String> busy = {};
  int _serial = 0;

  Uri _endpoint(String path) =>
      Uri.parse('${_apiBaseUrl.replaceFirst(RegExp(r'/$'), '')}$path');
  Map<String, String> _headers({bool json = false}) {
    final token = authorizationHeader();
    final headers = <String, String>{};
    if (json) headers['Content-Type'] = 'application/json';
    if (token != null) headers['Authorization'] = token;
    return headers;
  }

  bool contains(String activityId) =>
      plans.any((plan) => plan.activityId == activityId);

  void clear() {
    ++_serial;
    plans = const [];
    loading = false;
    failed = false;
    busy.clear();
    notifyListeners();
  }

  Future<void> load() async {
    if (!configured || authorizationHeader() == null) {
      clear();
      return;
    }
    final serial = ++_serial;
    loading = true;
    failed = false;
    notifyListeners();
    try {
      final response = await _client
          .get(_endpoint('/v1/me/activity-plans'), headers: _headers())
          .timeout(const Duration(seconds: 12));
      if (response.statusCode != 200) throw StateError('Plans unavailable');
      final rows =
          (jsonDecode(response.body) as Map<String, dynamic>)['data']
              as List<dynamic>;
      if (serial != _serial) return;
      plans = [
        for (final row in rows)
          ActivityPlan.fromJson(row as Map<String, dynamic>),
      ];
    } catch (_) {
      if (serial != _serial) return;
      failed = true;
    } finally {
      if (serial == _serial) {
        loading = false;
        notifyListeners();
      }
    }
  }

  Future<void> toggle(String activityId) async {
    if (!configured || authorizationHeader() == null) {
      throw StateError('Sign in to plan an activity');
    }
    if (busy.contains(activityId)) return;
    busy.add(activityId);
    notifyListeners();
    try {
      final existing = plans.where((plan) => plan.activityId == activityId);
      if (existing.isEmpty) {
        final response = await _client
            .post(
              _endpoint('/v1/me/activity-plans'),
              headers: _headers(json: true),
              body: jsonEncode({'activityId': activityId}),
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 201) throw StateError('Plan unavailable');
      } else {
        final response = await _client
            .delete(
              _endpoint(
                '/v1/me/activity-plans/${Uri.encodeComponent(existing.first.id)}',
              ),
              headers: _headers(),
            )
            .timeout(const Duration(seconds: 12));
        if (response.statusCode != 204) {
          throw StateError('Could not remove plan');
        }
      }
      await load();
    } finally {
      busy.remove(activityId);
      notifyListeners();
    }
  }

  @override
  void dispose() {
    ++_serial;
    _client.close();
    super.dispose();
  }
}

class MyActivitiesPage extends StatefulWidget {
  const MyActivitiesPage({super.key, required this.plans});
  final ActivityPlansController plans;

  @override
  State<MyActivitiesPage> createState() => _MyActivitiesPageState();
}

class _MyActivitiesPageState extends State<MyActivitiesPage> {
  @override
  void initState() {
    super.initState();
    unawaited(widget.plans.load());
  }

  Future<void> _remove(ActivityPlan plan) async {
    try {
      await widget.plans.toggle(plan.activityId);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Could not update your activity plan.')),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.plans,
    builder: (context, _) {
      final controller = widget.plans;
      if (!controller.configured) {
        return const Center(
          child: Text('My Activities requires the Birdtie API.'),
        );
      }
      if (controller.authorizationHeader() == null) {
        return const Center(child: Text('Sign in to see your activity plans.'));
      }
      if (controller.loading && controller.plans.isEmpty) {
        return const Center(child: CircularProgressIndicator());
      }
      if (controller.failed && controller.plans.isEmpty) {
        return Center(
          child: TextButton(
            onPressed: controller.load,
            child: const Text('My Activities unavailable. Tap to retry.'),
          ),
        );
      }
      return RefreshIndicator(
        onRefresh: controller.load,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 22, 16, 32),
          children: [
            const Text(
              'Your plans to attend. These are private reminders, not event registrations or confirmed participation.',
              style: TextStyle(color: Color(0xFF747B73)),
            ),
            const SizedBox(height: 20),
            if (controller.plans.isEmpty)
              const Padding(
                padding: EdgeInsets.all(22),
                child: Text(
                  'No activity plans yet. Add an upcoming Activity from an Agent result.',
                  textAlign: TextAlign.center,
                ),
              ),
            for (final plan in controller.plans)
              ListTile(
                leading: const Icon(Icons.event_outlined),
                title: Text(
                  plan.available ? plan.title : 'Unavailable activity',
                ),
                subtitle: Text(
                  plan.available
                      ? '${plan.cityId} · ${plan.status}${plan.startsAt == null ? '' : ' · ${plan.startsAt!.toLocal()}'}'
                      : 'No longer visible. You can remove this plan.',
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
                trailing: IconButton(
                  tooltip: 'Remove activity plan',
                  onPressed: controller.busy.contains(plan.activityId)
                      ? null
                      : () => _remove(plan),
                  icon: const Icon(Icons.event_busy_outlined),
                ),
              ),
          ],
        ),
      );
    },
  );
}
