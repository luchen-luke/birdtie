#pragma once

#include <cstddef>
#include <cstdint>

namespace birdtie_asr {
// Post-capture gate only. These conservative thresholds are not a VAD or a
// reason to stop recording. Phone silence/quiet-speech validation is required.
constexpr bool speech_energy(const int16_t *pcm, size_t count) {
    if (!pcm || count < 1600) return false;
    double total = 0;
    int voiced_frames = 0;
    for (size_t offset = 0; offset < count; offset += 320) {
        const size_t end = offset + 320 < count ? offset + 320 : count;
        double energy = 0;
        for (size_t i = offset; i < end; ++i) {
            const double value = static_cast<double>(pcm[i]) / 32768.0;
            energy += value * value;
        }
        total += energy;
        if (energy / static_cast<double>(end - offset) >= 0.006 * 0.006) ++voiced_frames;
    }
    return total / static_cast<double>(count) >= 0.003 * 0.003 && voiced_frames >= 5;
}
}
