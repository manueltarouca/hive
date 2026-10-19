cmake_minimum_required(VERSION 3.22)
project(hivevt C)

# HIVE_ROOT: the repository. libghostty-vt comes from `make vt-android`, the
# shim is the Mac app's own (apps/macos/Sources/CGhosttyVT).
set(VT ${HIVE_ROOT}/third_party/ghostty-vt-android)
set(SHIM ${HIVE_ROOT}/apps/macos/Sources/CGhosttyVT)
if(NOT EXISTS ${VT}/lib/libghostty-vt.a)
  message(FATAL_ERROR "libghostty-vt for Android is missing: run make vt-android at ${HIVE_ROOT}")
endif()

add_library(ghostty-vt STATIC IMPORTED)
set_target_properties(ghostty-vt PROPERTIES IMPORTED_LOCATION ${VT}/lib/libghostty-vt.a)

add_library(hivevt SHARED jni.c ${SHIM}/shim.c)
target_include_directories(hivevt PRIVATE ${VT}/include ${SHIM}/include)
target_compile_definitions(hivevt PRIVATE GHOSTTY_STATIC)
target_compile_options(hivevt PRIVATE -O2 -Wall)
# Android 15+ devices may use 16 KB pages.
target_link_options(hivevt PRIVATE -Wl,-z,max-page-size=16384 -Wl,--gc-sections)
target_link_libraries(hivevt ghostty-vt log)
