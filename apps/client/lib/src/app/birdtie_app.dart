import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';

import '../auth/authenticated_client.dart';
import '../auth/birdtie_auth_controller.dart';
import '../city/city_catalog_recovery.dart';
import '../city/public_city_controller.dart';
import '../city/exploration_city_store.dart';
import '../content/private_moment_controller.dart';
import '../workspace/map_workspace.dart';
import '../workspace/now_moment_editor_host.dart';
import 'birdtie_surfaces.dart';

class BirdtieApp extends StatelessWidget {
  const BirdtieApp({super.key});

  @override
  Widget build(BuildContext context) => NowMomentEditorScope(
    child: MaterialApp(
      title: 'Birdtie',
      debugShowCheckedModeBanner: false,
      locale: const Locale('zh', 'CN'),
      supportedLocales: const [Locale('zh', 'CN')],
      localizationsDelegates: const [
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
      theme: birdtieTheme(Brightness.light),
      darkTheme: birdtieTheme(Brightness.dark),
      home: const BirdtieShell(),
    ),
  );
}

class BirdtieShell extends StatefulWidget {
  const BirdtieShell({super.key});

  @override
  State<BirdtieShell> createState() => _BirdtieShellState();
}

class _BirdtieShellState extends State<BirdtieShell>
    with WidgetsBindingObserver {
  final _auth = BirdtieAuthController();
  late final PublicCityController _city;
  late final CityCatalogRecovery _cityRecovery;
  late final AuthenticatedClient _privateClient;
  late final PrivateMomentController _moments;
  bool _wasSignedIn = false;

  @override
  void initState() {
    super.initState();
    _city = PublicCityController(
      loadUnrequestedPlaces: false,
      authorizationHeader: () => _auth.authorizationHeader,
      explorationStore: const SecureExplorationCityStore(),
    );
    _privateClient = AuthenticatedClient(
      auth: _auth,
      onUnauthorized: (token) async {
        await _auth.recoverSession(expectedAuthorization: token);
      },
    );
    _moments = PrivateMomentController(
      authorizationHeader: () => _auth.authorizationHeader,
      ownerID: () => _auth.accountID,
      identityChanges: _auth,
      client: _privateClient,
    );
    _auth.addListener(_onAuthChange);
    WidgetsBinding.instance.addObserver(this);
    _cityRecovery = CityCatalogRecovery(_city);
    unawaited(_auth.initialize());
    _cityRecovery.start(
      foreground:
          WidgetsBinding.instance.lifecycleState == null ||
          WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed,
    );
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _cityRecovery.setForeground(state == AppLifecycleState.resumed);
    if (state == AppLifecycleState.resumed &&
        !_auth.signedIn &&
        _auth.sessionRecoveryFailed) {
      unawaited(_auth.recoverSession());
    }
  }

  void _onAuthChange() {
    if (_wasSignedIn == _auth.signedIn) return;
    _wasSignedIn = _auth.signedIn;
    unawaited(_city.loadActivities());
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _cityRecovery.dispose();
    _auth.removeListener(_onAuthChange);
    _city.dispose();
    _moments.dispose();
    _privateClient.close();
    _auth.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      MapWorkspace(city: _city, auth: _auth, moments: _moments);
}
