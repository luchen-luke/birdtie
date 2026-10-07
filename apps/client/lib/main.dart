import 'package:flutter/material.dart';

import 'src/app/birdtie_app.dart';
import 'src/config/birdtie_environment.dart';

void main() {
  BirdtieEnvironment.validate();
  runApp(const BirdtieApp());
}
