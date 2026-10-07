# Sourced by the black-box tests: exposes dot() on top of $DOT_BIN.
# ponytail: without DOT_BIN, the binary is rebuilt into a stable per-user, per-checkout temp dir that is never
# cleaned (a sourced file must not own the caller's EXIT trap); the go build cache keeps it fast.
if [ -z "${DOT_BIN:-}" ]; then
  _lib_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
  _lib_dir=${TMPDIR:-/tmp}/dot-test-bin-$(id -u)-$(printf %s "$_lib_root" | cksum | cut -d" " -f1)
  mkdir -p -m 700 "$_lib_dir"
  _lib_tmp=$_lib_dir/dot.$$
  (cd "$_lib_root" && CGO_ENABLED=0 go build -trimpath -o "$_lib_tmp" ./cmd/dot) || { echo "build de dot impossible" >&2; exit 1; }
  mv -f "$_lib_tmp" "$_lib_dir/dot"
  DOT_BIN=$_lib_dir/dot
  unset _lib_root _lib_dir _lib_tmp
fi
export DOT_BIN
dot() { "$DOT_BIN" "$@"; }

# inode:mtime of a file, with GNU stat or BSD stat (macOS).
stat_inode_mtime() { stat -c '%i:%Y' "$1" 2>/dev/null || stat -f '%i:%m' "$1"; }

# in_pty <log> <command line>: runs the command line under a pseudo-terminal with script(1),
# returning its exit status. util-linux takes -c, BSD script (macOS) takes the command as arguments.
in_pty() {
  if script -qec true /dev/null >/dev/null 2>&1; then
    SHELL=/bin/bash script -q -e -c "$2" "$1"
  else
    script -q -e "$1" /bin/bash -c "$2"
  fi
}

# A check that aborts the script under set -e names its line instead of exiting silently.
trap 'echo "FAIL ${BASH_SOURCE[0]##*/}:$LINENO : $BASH_COMMAND" >&2' ERR

# not <command>: a negated check that fails the script under set -e, which `! command` never does.
not() { if "$@"; then return 1; fi; }
