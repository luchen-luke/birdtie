cmake_minimum_required(VERSION 3.22)
project(birdtie_asr LANGUAGES C CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_POSITION_INDEPENDENT_CODE ON)
# Android/Flutter passes the custom build type "profile", whose native flags
# are otherwise empty. Apply to this directory and vendored CPU subtargets;
# keep debug symbols, warnings and hardening flags supplied by the toolchain.
string(TOLOWER "${CMAKE_BUILD_TYPE}" BIRDTIE_ASR_BUILD_TYPE)
if(BIRDTIE_ASR_BUILD_TYPE STREQUAL "profile" OR
   BIRDTIE_ASR_BUILD_TYPE STREQUAL "release" OR
   BIRDTIE_ASR_BUILD_TYPE STREQUAL "relwithdebinfo")
    add_compile_options(-O3 -DNDEBUG)
endif()
set(BUILD_SHARED_LIBS OFF CACHE BOOL "" FORCE)
set(WHISPER_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(WHISPER_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(WHISPER_BUILD_SERVER OFF CACHE BOOL "" FORCE)
set(WHISPER_CURL OFF CACHE BOOL "" FORCE)
set(WHISPER_COREML OFF CACHE BOOL "" FORCE)
set(WHISPER_OPENVINO OFF CACHE BOOL "" FORCE)
set(GGML_NATIVE OFF CACHE BOOL "" FORCE)
set(GGML_BACKEND_DL OFF CACHE BOOL "" FORCE)
set(GGML_OPENMP OFF CACHE BOOL "" FORCE)
set(GGML_BLAS OFF CACHE BOOL "" FORCE)
set(GGML_CUDA OFF CACHE BOOL "" FORCE)
set(GGML_VULKAN OFF CACHE BOOL "" FORCE)
set(GGML_METAL OFF CACHE BOOL "" FORCE)
set(GGML_CPU_KLEIDIAI OFF CACHE BOOL "" FORCE)
set(GGML_CCACHE OFF CACHE BOOL "" FORCE)
set(GGML_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(GGML_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
foreach(backend HIP MUSA WEBGPU ZDNN VIRTGPU VIRTGPU_BACKEND RPC SYCL OPENVINO ET ET_SYSEMU OPENCL HEXAGON ZENDNN)
    set(GGML_${backend} OFF CACHE BOOL "" FORCE)
endforeach()
set(GGML_OPENMP_FETCH OFF CACHE BOOL "" FORCE)
# No dependency provider/network operation is allowed during native compilation.
set(FETCHCONTENT_FULLY_DISCONNECTED ON CACHE BOOL "" FORCE)
set(FETCHCONTENT_UPDATES_DISCONNECTED ON CACHE BOOL "" FORCE)

add_subdirectory(third_party/whisper_cpp ${CMAKE_CURRENT_BINARY_DIR}/whisper)
add_library(birdtie_asr SHARED whisper_jni.cpp)
target_link_libraries(birdtie_asr PRIVATE whisper android)
# NDK r28 supports 16KB alignment; state it explicitly for this new ELF.
target_link_options(birdtie_asr PRIVATE "-Wl,-z,max-page-size=16384")
