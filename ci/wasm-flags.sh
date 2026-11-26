#!/bin/sh
# How D2 builds its WebAssembly module. Sourced by ci/build-wasm.sh and
# ci/test-wasm.sh.
#
# D2.js calls into the module and every call runs to completion, so the module
# is built without TinyGo's goroutine scheduler. On WebAssembly that scheduler
# depends on Binaryen's asyncify pass, which rewrites every function that could
# be running when a goroutine is switched out and made the code two thirds
# larger.
#
#   -scheduler=none          No scheduler, and so no asyncify.
#   -panic-unwind=explicit   recover still works: a panic returns through its
#                            callers, which check for it after each call.
#   -tags=goroutines.inert   A go statement starts nothing, where TinyGo would
#                            otherwise refuse to build. ci/wasm-goroutines.txt
#                            lists the ones in the module and why each can go
#                            unstarted.
#   -stack-size=4MB          Everything runs on one stack. With a scheduler each
#                            call, and each TALA seed and route flavor inside
#                            it, had 1 MB of its own.
#
# The tag, and calls from JavaScript into a module without a scheduler at all,
# need TinyGo from https://github.com/d2lang/tinygo, which
# ci/install-wasm-tools.sh installs.
case $(tinygo version) in
*-d2.*) ;;
*)
  echo "The WebAssembly build needs TinyGo from https://github.com/d2lang/tinygo (see ci/install-wasm-tools.sh), found: $(tinygo version)" >&2
  exit 1
  ;;
esac
wasm_flags="-target=wasm -scheduler=none -panic-unwind=explicit -tags=goroutines.inert -stack-size=4MB -no-debug"
