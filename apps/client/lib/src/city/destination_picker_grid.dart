import 'package:flutter/material.dart';

import 'destination_artwork.dart';
import 'destination_artwork_view.dart';
import 'public_city_controller.dart';

/// The catalog is a lazy shortcut, never a restriction on global search.
class DestinationPickerGrid extends StatelessWidget {
  const DestinationPickerGrid({
    super.key,
    required this.cities,
    required this.loader,
    required this.onSelect,
    this.selectedCityID,
  });
  final List<PublicCity> cities;
  final DestinationArtworkLoader loader;
  final ValueChanged<PublicCity> onSelect;
  final String? selectedCityID;
  @override
  Widget build(BuildContext context) => SliverLayoutBuilder(
    builder: (context, constraints) => SliverGrid(
      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
        crossAxisCount: constraints.crossAxisExtent >= 640 ? 3 : 2,
        mainAxisExtent: 196,
        crossAxisSpacing: 12,
        mainAxisSpacing: 8,
      ),
      delegate: SliverChildBuilderDelegate((context, index) {
        final city = cities[index];
        final selected = city.id == selectedCityID;
        final colors = Theme.of(context).colorScheme;
        return Semantics(
          selected: selected,
          button: true,
          label: city.name,
          child: Material(
            color: Colors.transparent,
            child: InkWell(
              key: Key('now-scope-city-${city.id}'),
              onTap: () => onSelect(city),
              borderRadius: BorderRadius.circular(12),
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 4),
                child: Column(
                  children: [
                    DecoratedBox(
                      decoration: BoxDecoration(
                        color: colors.surfaceContainerLow,
                        borderRadius: BorderRadius.circular(18),
                        border: selected
                            ? Border.all(color: colors.primary)
                            : null,
                      ),
                      child: DestinationArtworkView(
                        cityID: city.id,
                        artwork: city.artwork,
                        hasRemoteArtwork: city.artworkDeclared,
                        loader: loader,
                      ),
                    ),
                    const SizedBox(height: 6),
                    Text(
                      city.name,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.titleSmall,
                    ),
                    if (city.region.isNotEmpty)
                      Padding(
                        padding: const EdgeInsets.only(top: 2),
                        child: Text(
                          city.region,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          textAlign: TextAlign.center,
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                      ),
                  ],
                ),
              ),
            ),
          ),
        );
      }, childCount: cities.length),
    ),
  );
}
