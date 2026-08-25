cmake_minimum_required(VERSION 3.16)
project(fut15_p2p_hook LANGUAGES C CXX)

# Cross-compiled to a Windows x86-64 DLL (built on Linux with mingw-w64, run under
# Proton). Phase 0: observe the game's UDP/P2P socket traffic from inside the client.

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Vendored hooking engine (small, MIT). Fetched at configure time.
include(FetchContent)
FetchContent_Declare(
  minhook
  GIT_REPOSITORY https://github.com/TsudaKageyu/minhook.git
  GIT_TAG        master
)
FetchContent_MakeAvailable(minhook)

# Output must be named dinput8.dll (DLL-hijack load target), with no lib prefix
# and the plain name so the game loads it.
add_library(dinput8 SHARED src/dllmain.cpp)
target_link_libraries(dinput8 PRIVATE minhook ws2_32)
set_target_properties(dinput8 PROPERTIES
  PREFIX ""
  OUTPUT_NAME "dinput8"
  SUFFIX ".dll"
)

# Static-link the mingw runtimes so the DLL has no libgcc/libstdc++ dependency
# in the Proton prefix.
target_link_options(dinput8 PRIVATE -static -static-libgcc -static-libstdc++
  -Wl,--enable-stdcall-fixup)
