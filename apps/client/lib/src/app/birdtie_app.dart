import 'dart:async';

import 'package:flutter/material.dart';

import '../auth/birdtie_auth_controller.dart';
import '../city/public_city_controller.dart';
import '../content/private_moment_controller.dart';
import '../workspace/map_workspace.dart';

class BirdtieApp extends StatelessWidget {
  const BirdtieApp({super.key});

  @override
  Widget build(BuildContext context) => MaterialApp(
    title: 'Birdtie',
    debugShowCheckedModeBanner: false,
    theme: ThemeData(
      useMaterial3: true,
      colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF193B32)),
      scaffoldBackgroundColor: const Color(0xFFFCFBF8),
      fontFamily: 'Aptos',
    ),
    home: const BirdtieShell(),
  );
}

class BirdtieShell extends StatefulWidget {
  const BirdtieShell({super.key});

  @override
  State<BirdtieShell> createState() => _BirdtieShellState();
}

class _BirdtieShellState extends State<BirdtieShell> {
  final _auth = BirdtieAuthController();
  late final PublicCityController _city;
  late final PrivateMomentController _moments;
  bool _wasSignedIn = false;

  @override
  void initState() {
    super.initState();
    _city = PublicCityController(
      authorizationHeader: () => _auth.authorizationHeader,
    );
    _moments = PrivateMomentController(
      authorizationHeader: () => _auth.authorizationHeader,
    );
    _auth.addListener(_onAuthChange);
    unawaited(_auth.initialize());
    unawaited(_city.loadCities());
  }

  void _onAuthChange() {
    if (_wasSignedIn == _auth.signedIn) return;
    _wasSignedIn = _auth.signedIn;
    unawaited(_city.loadActivities());
    unawaited(_moments.refresh());
  }

  @override
  void dispose() {
    _auth.removeListener(_onAuthChange);
    _auth.dispose();
    _city.dispose();
    _moments.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      MapWorkspace(city: _city, auth: _auth, moments: _moments);
}
