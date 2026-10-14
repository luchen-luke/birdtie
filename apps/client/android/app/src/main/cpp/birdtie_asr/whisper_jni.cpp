#include <jni.h>
#include <whisper.h>
#include <ggml.h>
#include "energy_gate.h"

#include <atomic>
#include <cmath>
#include <cstdint>
#include <memory>
#include <mutex>
#include <string>
#include <unordered_map>
#include <vector>

namespace {
constexpr jsize kMaximumSamples = 16000 * 30;
struct Session {
    std::atomic<bool> cancelled{false};
    std::atomic<int> detected_language{-1};
    whisper_context *context = nullptr;
    ~Session() { if (context) whisper_free(context); }
};
struct AudioBuffers {
    std::vector<jshort> pcm;
    std::vector<float> samples;
    ~AudioBuffers();
};
struct Transcript {
    std::string value;
    ~Transcript();
};
struct UnicodeBuffer {
    std::vector<jchar> value;
    ~UnicodeBuffer();
};
std::mutex sessions_mutex;
std::unordered_map<jlong, std::shared_ptr<Session>> sessions;
std::atomic<jlong> next_handle{1};

void quiet_log(enum ggml_log_level, const char *, void *) {}
bool abort_decode(void *data) { return static_cast<Session *>(data)->cancelled.load(); }
template <typename T> void wipe(std::vector<T> &values) {
    volatile T *p = values.data();
    for (size_t i = 0; i < values.size(); ++i) p[i] = T{};
}
void wipe(std::string &text) {
    volatile char *p = text.empty() ? nullptr : &text[0];
    for (size_t i = 0; i < text.size(); ++i) p[i] = 0;
    text.clear();
}
AudioBuffers::~AudioBuffers() { wipe(pcm); wipe(samples); }
Transcript::~Transcript() { wipe(value); }
UnicodeBuffer::~UnicodeBuffer() { wipe(value); }
struct UtfChars {
    JNIEnv *env;
    jstring input;
    const char *value;
    UtfChars(JNIEnv *e, jstring s) : env(e), input(s), value(e->GetStringUTFChars(s, nullptr)) {}
    ~UtfChars() { if (value) env->ReleaseStringUTFChars(input, value); }
};
std::shared_ptr<Session> session(jlong handle) {
    std::lock_guard<std::mutex> lock(sessions_mutex);
    auto found = sessions.find(handle);
    return found == sessions.end() ? nullptr : found->second;
}
jstring unicode_text(JNIEnv *env, const std::string &utf8) {
    // NewStringUTF expects modified UTF-8, not Whisper's standard UTF-8.
    // Decode strict UTF-8, including four-byte code points, to a wiped UTF-16
    // temporary. Invalid decoder bytes never become a replacement transcript.
    UnicodeBuffer decoded;
    decoded.value.reserve(utf8.size());
    for (size_t i = 0; i < utf8.size();) {
        const uint8_t first = static_cast<uint8_t>(utf8[i++]);
        uint32_t point = 0;
        size_t extra = 0;
        uint32_t minimum = 0;
        if (first < 0x80) point = first;
        else if (first >= 0xc2 && first <= 0xdf) { point = first & 0x1f; extra = 1; minimum = 0x80; }
        else if (first >= 0xe0 && first <= 0xef) { point = first & 0x0f; extra = 2; minimum = 0x800; }
        else if (first >= 0xf0 && first <= 0xf4) { point = first & 0x07; extra = 3; minimum = 0x10000; }
        else return nullptr;
        if (i + extra > utf8.size()) return nullptr;
        for (size_t n = 0; n < extra; ++n) {
            const uint8_t next = static_cast<uint8_t>(utf8[i++]);
            if ((next & 0xc0) != 0x80) return nullptr;
            point = (point << 6) | (next & 0x3f);
        }
        if (point < minimum || point > 0x10ffff || (point >= 0xd800 && point <= 0xdfff)) return nullptr;
        if (point <= 0xffff) decoded.value.push_back(static_cast<jchar>(point));
        else {
            point -= 0x10000;
            decoded.value.push_back(static_cast<jchar>(0xd800 + (point >> 10)));
            decoded.value.push_back(static_cast<jchar>(0xdc00 + (point & 0x3ff)));
        }
    }
    const jchar empty = 0;
    return env->NewString(decoded.value.empty() ? &empty : decoded.value.data(), static_cast<jsize>(decoded.value.size()));
}
}

extern "C" JNIEXPORT jint JNICALL JNI_OnLoad(JavaVM *, void *) {
    whisper_log_set(quiet_log, nullptr);
    ggml_log_set(quiet_log, nullptr);
    return JNI_VERSION_1_6;
}
extern "C" JNIEXPORT jlong JNICALL
Java_app_civu_civu_1mobile_BundledOfflineSpeech_nativeCreate(JNIEnv *, jobject) {
    try {
    jlong handle = next_handle.fetch_add(1);
    if (handle <= 0) return 0;
    std::lock_guard<std::mutex> lock(sessions_mutex);
    sessions.emplace(handle, std::make_shared<Session>());
    return handle;
    } catch (...) { return 0; }
}
extern "C" JNIEXPORT jboolean JNICALL
Java_app_civu_civu_1mobile_BundledOfflineSpeech_nativePrepare(JNIEnv *env, jobject, jlong handle, jstring path) {
    try {
    auto job = session(handle);
    if (!job || job->cancelled.load() || !path) return JNI_FALSE;
    UtfChars model(env, path);
    if (!model.value) return JNI_FALSE;
    whisper_context_params options = whisper_context_default_params();
    options.use_gpu = false;
    if (job->context) return JNI_FALSE;
    job->context = whisper_init_from_file_with_params(model.value, options);
    return job->context && !job->cancelled.load() ? JNI_TRUE : JNI_FALSE;
    } catch (...) { return JNI_FALSE; }
}
extern "C" JNIEXPORT jstring JNICALL
Java_app_civu_civu_1mobile_BundledOfflineSpeech_nativeTranscribe(JNIEnv *env, jobject, jlong handle, jshortArray audio, jint count) {
    try {
    auto job = session(handle);
    if (job) job->detected_language.store(-1);
    if (!job || !job->context || job->cancelled.load() || !audio || count <= 0 ||
        count > kMaximumSamples || count > env->GetArrayLength(audio)) return nullptr;
    AudioBuffers buffers;
    buffers.pcm.resize(count);
    env->GetShortArrayRegion(audio, 0, count, buffers.pcm.data());
    if (env->ExceptionCheck()) return nullptr;
    if (!birdtie_asr::speech_energy(buffers.pcm.data(), buffers.pcm.size())) return unicode_text(env, "");
    buffers.samples.resize(count);
    for (jint i = 0; i < count; ++i) buffers.samples[i] = static_cast<float>(buffers.pcm[i]) / 32768.0f;
    wipe(buffers.pcm);
    auto parameters = whisper_full_default_params(WHISPER_SAMPLING_BEAM_SEARCH);
    parameters.beam_search.beam_size = 5;
    parameters.greedy.best_of = 5;
    parameters.language = "auto";
    parameters.translate = false;
    parameters.no_context = true;
    parameters.n_threads = 2;
    parameters.print_realtime = false;
    parameters.print_progress = false;
    parameters.print_timestamps = false;
    parameters.print_special = false;
    parameters.debug_mode = false;
    parameters.abort_callback = abort_decode;
    parameters.abort_callback_user_data = job.get();
    int status = whisper_full(job->context, parameters, buffers.samples.data(), count);
    wipe(buffers.samples);
    if (status != 0 || job->cancelled.load()) return nullptr;
    Transcript text;
    text.value.reserve(16384);
    for (int i = 0; i < whisper_full_n_segments(job->context); ++i) {
        float non_speech = whisper_full_get_segment_no_speech_prob(job->context, i);
        if (!std::isfinite(non_speech) || non_speech >= 0.6f) continue;
        const char *segment = whisper_full_get_segment_text(job->context, i);
        if (segment) {
            const size_t length = std::char_traits<char>::length(segment);
            if (length > 16384 - text.value.size()) return nullptr;
            text.value.append(segment, length);
        }
    }
    jstring result = job->cancelled.load() ? nullptr : unicode_text(env, text.value);
    if (result && !job->cancelled.load() && !text.value.empty()) {
        const int language = whisper_full_lang_id(job->context);
        if (language >= 0 && whisper_lang_str(language)) job->detected_language.store(language);
    }
    return result;
    } catch (...) { return nullptr; }
}
extern "C" JNIEXPORT jstring JNICALL
Java_app_civu_civu_1mobile_BundledOfflineSpeech_nativeDetectedLanguage(JNIEnv *env, jobject, jlong handle) {
    try {
    auto job = session(handle);
    if (!job || job->cancelled.load()) return nullptr;
    const int language = job->detected_language.load();
    if (language < 0) return nullptr;
    const char *code = whisper_lang_str(language);
    if (!code) return nullptr;
    jstring result = unicode_text(env, code);
    return job->cancelled.load() ? nullptr : result;
    } catch (...) { return nullptr; }
}
extern "C" JNIEXPORT void JNICALL
Java_app_civu_civu_1mobile_BundledOfflineSpeech_nativeCancel(JNIEnv *, jobject, jlong handle) {
    try { if (auto job = session(handle)) { job->cancelled.store(true); job->detected_language.store(-1); } } catch (...) {}
}
extern "C" JNIEXPORT void JNICALL
Java_app_civu_civu_1mobile_BundledOfflineSpeech_nativeRelease(JNIEnv *, jobject, jlong handle) {
    try {
    std::lock_guard<std::mutex> lock(sessions_mutex);
    // Inference owns a shared reference; context is freed only after it returns.
    sessions.erase(handle);
    } catch (...) {}
}
