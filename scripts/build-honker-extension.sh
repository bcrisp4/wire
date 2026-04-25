#!/usr/bin/env bash
# Builds the Honker SQLite extension as a cdylib and drops it at ./build/libhonker_ext.{so,dylib}.
# Re-run after upgrading the honker-go version pinned in go.mod.
set -euo pipefail

# Make cargo available to non-interactive shells (rustup default install path).
if ! command -v cargo >/dev/null 2>&1 && [[ -f "$HOME/.cargo/env" ]]; then
  # shellcheck disable=SC1091
  . "$HOME/.cargo/env"
fi
if ! command -v cargo >/dev/null 2>&1; then
  echo "ERROR: cargo not found. Install Rust via https://rustup.rs/ then re-run." >&2
  exit 1
fi

HONKER_REPO="${HONKER_REPO:-https://github.com/russellromney/honker.git}"
HONKER_REF="${HONKER_REF:-main}"
SRC_DIR="${SRC_DIR:-./build/honker-src}"
OUT_DIR="${OUT_DIR:-./build}"

mkdir -p "$OUT_DIR"

if [[ ! -d "$SRC_DIR/.git" ]]; then
  echo ">> cloning $HONKER_REPO @ $HONKER_REF -> $SRC_DIR"
  git clone --depth 1 --branch "$HONKER_REF" "$HONKER_REPO" "$SRC_DIR"
else
  echo ">> updating $SRC_DIR"
  git -C "$SRC_DIR" fetch --depth 1 origin "$HONKER_REF"
  git -C "$SRC_DIR" checkout FETCH_HEAD
fi

echo ">> cargo build -p honker-extension --release"
( cd "$SRC_DIR" && cargo build -p honker-extension --release )

UNAME=$(uname -s)
case "$UNAME" in
  Linux*)   EXT="so"  ;;
  Darwin*)  EXT="dylib" ;;
  *) echo "unsupported OS: $UNAME" >&2; exit 1 ;;
esac

# The honker-extension crate's [lib] name is "honker_ext" (per its Cargo.toml),
# so cargo produces libhonker_ext.{so,dylib}. The convention required by the
# load_extension() SQL function is to omit the "lib" prefix and the extension,
# i.e. callers point at "$OUT_DIR/honker_ext".
SRC_LIB="$SRC_DIR/target/release/libhonker_ext.$EXT"
DEST_LIB="$OUT_DIR/libhonker_ext.$EXT"
if [[ ! -f "$SRC_LIB" ]]; then
  echo "ERROR: expected build artifact not found at $SRC_LIB" >&2
  echo "Listing $SRC_DIR/target/release/ for debugging:"
  ls -la "$SRC_DIR/target/release/" || true
  exit 1
fi
cp -f "$SRC_LIB" "$DEST_LIB"
echo ">> installed: $DEST_LIB"
