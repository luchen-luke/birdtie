import 'package:web/web.dart' as web;

void openMapAttribution(String address) => web.window.open(address, '_blank');

void openExternalSource(String address) {
  final uri = Uri.tryParse(address);
  if (uri != null && (uri.scheme == 'https' || uri.scheme == 'http')) {
    web.window.open(address, '_blank', 'noopener,noreferrer');
  }
}
