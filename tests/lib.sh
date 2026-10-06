# Sourced by the black-box tests: exposes dot() on top of $DOT_BIN.
# ponytail: without DOT_BIN, the binary is rebuilt into a stable per-user temp dir that is never
# cleaned (a sourced file must not own the caller's EXIT trap); the go build cache keeps it fast.
if [ -z "${DOT_BIN:-}" ]; then
  _lib_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
  _lib_dir=${TMPDIR:-/tmp}/dot-test-bin-$(id -u)
  mkdir -p -m 700 "$_lib_dir"
  _lib_tmp=$_lib_dir/dot.$$
  (cd "$_lib_root" && CGO_ENABLED=0 go build -trimpath -o "$_lib_tmp" ./cmd/dot) || { echo "build de dot impossible" >&2; exit 1; }
  mv -f "$_lib_tmp" "$_lib_dir/dot"
  DOT_BIN=$_lib_dir/dot
  unset _lib_root _lib_dir _lib_tmp
fi
export DOT_BIN
dot() { "$DOT_BIN" "$@"; }
