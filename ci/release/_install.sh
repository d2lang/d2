#!/bin/sh
set -eu

cd -- "$(dirname "$0")/../sub/lib"
. ./log.sh
. ./flag.sh
. ./release.sh
# The generated installer embeds these reviewed manifests.
release_checksums() {
  cat "$(dirname "$0")"/checksums/*.txt
}
cd - >/dev/null

if [ -z "${D2_RELEASE_CHECKSUM-}" ]; then
  . "$(dirname "$0")/checksum.sh"
fi
if ! command -v installer_gh >/dev/null 2>&1; then
  . "$(dirname "$0")/verifier.sh"
fi

help() {
  arg0="$0"
  if [ "$0" = sh ]; then
    arg0="curl -fsSL https://d2lang.com/install.sh | sh -s --"
  fi

  cat <<EOF
usage: $arg0 [-d|--dry-run] [--version vX.X.X] [--method detect] [--prefix path]
  [--verify-provenance] [--force] [--uninstall] [-x|--trace]

install.sh automates the installation of D2 onto your system. It currently only supports
the installation of standalone releases from GitHub and via Homebrew on macOS. See the
docs for --detect below for more information

\$PREFIX in the docs below refers to the path set by --prefix. See docs on the --prefix
flag below for the default.

Flags:

-d, --dry-run
  Pass to have install.sh show the install method and flags that will be used to install
  without executing them. Very useful to understand what changes it will make to your system.

--version vX.X.X
  Pass to have install.sh install the given version instead of the latest version.
  warn: The version may not be obeyed with package manager installations. Use
        --method=standalone to enforce the version.

--verify-provenance
  Select a standalone install and independently verify the signed build provenance on
  this machine, even when the archive's verified SHA-256 is embedded in this installer.
  Requires a release with signed provenance (v0.8.2 or later).

--method [detect | standalone | homebrew ]
  Pass to control the method by which to install. Right now we only support standalone
  releases from GitHub but later we'll add support for brew, rpm, deb and more.

  - detect will use your OS's package manager automatically.
    So far it only detects macOS and automatically uses homebrew.
  - homebrew uses https://brew.sh/ which is a macOS and Linux package manager.
  - standalone installs a standalone release archive into the unix hierarchy path
     specified by --prefix

--prefix path
  Controls the unix hierarchy path into which standalone releases are installed.
  Defaults to /usr/local or ~/.local if /usr/local is not writable by the current user.
  Remember that whatever you use, you must have the bin directory of your prefix path in
  \$PATH to execute the d2 binary. For example, if my prefix directory is /usr/local then
  my \$PATH must contain /usr/local/bin.
  You may also need to include \$PREFIX/share/man into \$MANPATH.
  install.sh will tell you whether \$PATH or \$MANPATH need to be updated after successful
  installation.

--tala [legacy-version] (unsupported)
  --tala is no longer supported. This installer installs only D2 and rejects
  legacy TALA installation requests before changing your installation.
  For releases with bundled TALA, omit --tala and verify availability with d2 layout.
  For older releases, install the legacy plugin separately:
  https://github.com/terrastruct/TALA/releases

--force:
  Reinstall even if the versions match. The installer stages the verified replacement
  before switching the executable. Downloaded archives remain in the release cache.

--uninstall:
  Uninstall the installed version of d2. The --method and --prefix flags must be the same
  as for installation. i.e if you used --method standalone you must again use --method
  standalone for uninstallation. With detect, the install script will try to use the OS
  package manager to uninstall instead. Use --version, --force, and --verify-provenance
  only when installing.

-x, --trace:
  Run script with set -x.

All downloaded archives are cached into ~/.cache/d2/release. use \$XDG_CACHE_HOME to change
path of the cached assets. Release archives are unarchived into \$PREFIX/lib/d2/d2-<VERSION>

note: Deleting the unarchived releases will cause --uninstall to stop working.

You can rerun install.sh to update your version of D2. install.sh will avoid reinstalling
if the installed version is the latest unless --force is passed.

See https://github.com/d2lang/d2/blob/master/docs/INSTALL.md#security for
documentation on its security.
EOF
}

main() {
  install_only_flag=
  while flag_parse "$@"; do
    case "$FLAG" in
      h|help)
        help
        return 0
        ;;
      d|dry-run)
        flag_noarg && shift "$FLAGSHIFT"
        DRY_RUN=1
        ;;
      version)
        flag_nonemptyarg && shift "$FLAGSHIFT"
        VERSION=$FLAGARG
        install_only_flag=--version
        ;;
      verify-provenance)
        flag_noarg && shift "$FLAGSHIFT"
        VERIFY_PROVENANCE=1
        install_only_flag=--verify-provenance
        ;;
      tala)
        shift "$FLAGSHIFT"
        TALA_REQUEST=1
        ;;
      method)
        flag_nonemptyarg && shift "$FLAGSHIFT"
        METHOD=$FLAGARG
        ;;
      prefix)
        flag_nonemptyarg && shift "$FLAGSHIFT"
        export PREFIX=$FLAGARG
        ;;
      force)
        flag_noarg && shift "$FLAGSHIFT"
        FORCE=1
        install_only_flag=--force
        ;;
      uninstall)
        flag_noarg && shift "$FLAGSHIFT"
        UNINSTALL=1
        ;;
      x|trace)
        flag_noarg && shift "$FLAGSHIFT"
        set -x
        export TRACE=1
        ;;
      *)
        flag_errusage "unrecognized flag $FLAGRAW"
        ;;
    esac
  done
  shift "$FLAGSHIFT"

  if [ $# -gt 0 ]; then
    flag_errusage "no arguments are accepted"
  fi

  if [ -n "${UNINSTALL-}" ] && [ -n "$install_only_flag" ]; then
    flag_errusage "$install_only_flag cannot be used with --uninstall"
  fi

  if [ -n "${TALA_REQUEST-}" ]; then
    flag_errusage "--tala is no longer supported; this installer installs only D2.
For releases with bundled TALA, omit --tala and verify availability with d2 layout.
For older releases, install the legacy plugin separately: https://github.com/terrastruct/TALA/releases"
  fi

  REPO=${REPO:-d2lang/d2}
  ensure_os
  ensure_arch
  ensure_prefix
  CACHE_DIR=$(cache_dir)
  mkdir -p "$CACHE_DIR"
  METHOD=${METHOD:-detect}
  INSTALL_DIR=$PREFIX/lib/d2
  standalone_binary=$PREFIX/bin/d2
  if [ "$OS" = windows ]; then standalone_binary=$standalone_binary.exe; fi

  if [ -z "${UNINSTALL-}" ] && [ -n "${VERIFY_PROVENANCE-}" ]; then
    case "$METHOD" in
      detect) METHOD=standalone ;;
      homebrew) flag_errusage "--verify-provenance requires a standalone installation" ;;
    esac
  fi

  case $METHOD in
    detect)
      case "$OS" in
        macos)
          if command -v brew >/dev/null; then
            log "detected macOS with homebrew, using homebrew for installation"
            METHOD=homebrew
          else
            warn "detected macOS without homebrew, falling back to --method=standalone"
            METHOD=standalone
          fi
          ;;
        linux|windows)
          METHOD=standalone
          ;;
        *)
          warn "unrecognized OS $OS, falling back to --method=standalone"
          METHOD=standalone
          ;;
      esac
      ;;
    standalone) ;;
    homebrew) ;;
    *)
      echoerr "unknown installation method $METHOD"
      return 1
      ;;
  esac

  if [ -n "${UNINSTALL-}" ]; then
    uninstall
    if [ -n "${DRY_RUN-}" ]; then
      bigheader "Rerun without --dry-run to execute printed commands and perform install."
    fi
  else
    install
    if [ -n "${DRY_RUN-}" ]; then
      bigheader "Rerun without --dry-run to execute printed commands and perform install."
    fi
  fi
}

install() {
  case $METHOD in
    standalone)
      install_d2_standalone
      ;;
    homebrew)
      install_d2_brew
      ;;
  esac

  FGCOLOR=2 bigheader 'next steps'
  case $METHOD in
    standalone) install_post_standalone ;;
    homebrew) install_post_brew ;;
  esac
  install_post_warn
}

install_post_standalone() {
  log "d2-$VERSION-$OS-$ARCH has been successfully installed into $PREFIX"
  log "Rerun this install script with --uninstall to uninstall."
  log
  if ! echo "$PATH" | grep -qF "$PREFIX/bin"; then
    logcat >&2 <<EOF
Extend your \$PATH to use d2:
  export PATH=$PREFIX/bin:\$PATH
Then run:
  d2 --help
EOF
  else
    log "Run d2 --help for usage."
  fi
  if ! manpath 2>/dev/null | grep -qF "$PREFIX/share/man"; then
    logcat >&2 <<EOF
Extend your \$MANPATH to view d2's manpages:
  export MANPATH=$PREFIX/share/man:\$MANPATH
Then run:
  man d2
EOF
  else
    log "Run man d2 for detailed docs."
  fi
}

install_post_brew() {
  log "d2 has been successfully installed with homebrew."
  log "Rerun this install script with --uninstall to uninstall."
  log
  log "Run d2 --help for usage."
  log "Run man d2 for detailed docs."

  VERSION=$(brew info d2 | head -n1 | cut -d' ' -f4)
  VERSION=${VERSION%,}
}

install_post_warn() {
  if command -v d2 >/dev/null; then
    INSTALLED_VERSION=$(d2 --version)
    if [ "${INSTALLED_VERSION#v}" != "${VERSION#v}" ]; then
      warn "newly installed d2 $VERSION is shadowed by d2 $INSTALLED_VERSION in \$PATH"
    fi
  fi
}

install_d2_standalone() {
  VERSION=${VERSION:-latest}
  requested_version=$VERSION
  header "installing d2-$VERSION"

  ensure_version

  # These published tags predate archive attestations. Missing evidence for any
  # other tag is an error, including when GitHub resolves "latest" to an old tag.
  legacy_release=
  case "$VERSION" in
    v0.0.1[23]|v0.1.[0-6]|v0.2.[0-6]|v0.3.0|v0.4.[0-2]|v0.5.[01]|v0.6.[0-9]|v0.7.[01])
      if [ "$requested_version" = latest ] || [ "$REPO" != d2lang/d2 ] || [ -n "${VERIFY_PROVENANCE-}" ]; then
        echoerr "refusing to install $REPO $VERSION without signed build provenance"
        return 1
      fi
      legacy_release=1
      ;;
  esac

  if [ -z "${FORCE-}" ] && [ -x "$standalone_binary" ] && [ -f "$standalone_binary" ] &&
    [ "$("$standalone_binary" --version)" = "$VERSION" ]; then
    log "skipping installation as d2 $VERSION is already installed in $PREFIX."
    return 0
  fi
  read_standalone_install

  ARCHIVE="d2-$VERSION-$OS-$ARCH.tar.gz"
  log "installing standalone release $ARCHIVE from github"

  asset_url="https://github.com/$REPO/releases/download/$VERSION/$ARCHIVE"
  # Verify and extract the same private copy, even when another install updates the cache.
  stage=$(mktempd)
  staged_archive=$stage/$ARCHIVE
  pinned_digest=$(release_checksums | awk -v name="$ARCHIVE" '$2 == name { print $1 }')
  fetch_release_asset \
    "$REPO" "$VERSION" "$ARCHIVE" "$asset_url" "$CACHE_DIR/$ARCHIVE" "$staged_archive" "$pinned_digest"
  if [ -n "$legacy_release" ]; then
    warn "$VERSION predates signed D2 archive provenance"
  elif [ -z "$pinned_digest" ] || [ -n "${VERIFY_PROVENANCE-}" ]; then
    if [ -n "${DRY_RUN-}" ]; then
      log "would verify signed build provenance for $ARCHIVE before installation"
    else
      verify_archive_provenance "$stage" "$staged_archive"
    fi
  fi

  if [ -n "${DRY_RUN-}" ]; then
    log "would stage d2-$VERSION and replace the executable under $PREFIX"
    return
  fi
  tar -C "$stage" -xzf "$staged_archive"
  mkdir "$stage/prefix"
  # Older archive recipes require a prefix without spaces. The private prefix
  # is a sibling of the extracted tree, including when TMPDIR contains spaces.
  (cd "$stage/d2-$VERSION" && make install PREFIX=../prefix)
  # Publication and uninstall own these two paths. A newer release recipe
  # which installs additional files requires an installer that can manage them.
  extra=$(cd "$stage/prefix" && find . ! -type d ! -path "./bin/${standalone_binary##*/}" ! -path './share/man/man1/d2.1' -print)
  if [ -n "$extra" ]; then
    echoerr "Release $VERSION installs additional files. Update install.sh to install it:"
    printf '%s\n' "$extra" >&2
    return 1
  fi
  update_standalone_prefix install
}

verify_archive_provenance() (
  stage=$1
  archive_path=$2
  # The first two attested releases predate the downloadable release sidecar.
  case "$VERSION" in
    v0.8.2) bundle_url=https://github.com/d2lang/d2/attestations/42650843/download ;;
    v0.9.0) bundle_url=https://github.com/d2lang/d2/attestations/45830930/download ;;
    *) bundle_url="https://github.com/$REPO/releases/download/$VERSION/d2-$VERSION-provenance.sigstore.json" ;;
  esac
  log "verifying signed D2 build provenance for $ARCHIVE"
  curl -qfsSL --proto '=https' --proto-redir '=https' -o "$stage/provenance.json" "$bundle_url"
  verifier=$(installer_gh "$stage")

  # A single signed statement covers all platforms. Bind the digest to the
  # selected filename and pin D2's permanent repository ID for offline verification.
  GH_HOST=github.com "$verifier" attestation verify "$archive_path" \
    --bundle "$stage/provenance.json" --repo d2lang/d2 \
    --signer-workflow d2lang/d2/.github/workflows/release-archives.yml \
    --source-ref "refs/tags/$VERSION" --predicate-type https://slsa.dev/provenance/v1 \
    --format json --jq '.[].verificationResult | select(.signature.certificate.sourceRepositoryIdentifier == "533087958") | .statement.subject[] | [.digest.sha256, .name] | @tsv' \
    >"$stage/subjects"
  archive_digest=$(sha256_file "$archive_path")
  if ! grep -Fxq "$(printf '%s\t%s' "$archive_digest" "$ARCHIVE")" "$stage/subjects"; then
    echoerr "signed D2 provenance does not match the file name and SHA-256 of $ARCHIVE"
    return 1
  fi
)

install_d2_brew() {
  header "installing d2 with homebrew"
  sh_c brew update
  sh_c brew install d2
}

uninstall() {
  INSTALLED_VERSION=
  if [ "$METHOD" = standalone ]; then
    read_standalone_install
  elif command -v d2 >/dev/null; then
    INSTALLED_VERSION=$(d2 --version)
  fi
  if [ -z "$INSTALLED_VERSION" ]; then
    warn "no version of d2 installed"
    return 0
  fi

  header "uninstalling d2-$INSTALLED_VERSION"
  case $METHOD in
    standalone) uninstall_d2_standalone ;;
    homebrew) uninstall_d2_brew ;;
  esac
}

read_standalone_install() {
  INSTALLED_VERSION=
  expected_binary=
  if [ -f "$standalone_binary" ]; then
    # Snapshot first so a concurrent rename cannot pair an old version with a new checksum.
    expected_binary=$(cksum < "$standalone_binary")
    if [ -x "$standalone_binary" ]; then
      INSTALLED_VERSION=$("$standalone_binary" --version) || INSTALLED_VERSION=
    fi
  fi
}

uninstall_d2_standalone() {
  log "uninstalling standalone release of d2-$INSTALLED_VERSION"

  if [ ! -e "$INSTALL_DIR/d2-$INSTALLED_VERSION" ]; then
    warn "missing standalone install release directory $INSTALL_DIR/d2-$INSTALLED_VERSION"
    warn "d2 must have been installed via some other installation method."
    return 1
  fi

  if [ -n "${DRY_RUN-}" ]; then
    log "would uninstall d2-$INSTALLED_VERSION from $PREFIX"
    return
  fi
  update_standalone_prefix uninstall
}

# Only the final changes to the selected prefix may need elevation. Pass paths
# as arguments to the child shell, including when the shared helper uses sudo.
update_standalone_prefix() (
  ensure_prefix_sh_c
  action=$1
  export action PREFIX OS INSTALLED_VERSION expected_binary
  export stage="${stage-}" VERSION="${VERSION-}"
  "$sh_c" 'sh -s -- "$action" "$PREFIX" "$OS" "$INSTALLED_VERSION" "$stage" "$VERSION" "$expected_binary"' <<'PUBLISH'
set -eu
action=$1 prefix=$2 os=$3 previous=$4 stage=$5 version=$6 expected_binary=$7
case "$prefix" in /*|[A-Za-z]:/*) ;; *) prefix=$PWD/$prefix ;; esac
lib=$prefix/lib/d2
lock=$lib/.install-lock
case "$previous" in
  *[!A-Za-z0-9.-]*)
    if [ "$action" = uninstall ]; then echo >&2 "Invalid installed D2 version: $previous"; exit 1; fi
    previous= ;; # A source build on PATH may have no managed release tree.
esac
mkdir -p "$lib"
# Set permissions at creation. Git Bash can create the directory but fail a
# subsequent chmod from mkdir -m, leaving a lock behind on a fresh install.
if ! (umask 077 && mkdir "$lock"); then
  if [ -d "$lock" ]; then
    echo >&2 "Cannot update $prefix while $lock exists"
    echo >&2 "Wait for the other installer to finish. If it was interrupted, inspect the lock directory for a saved release tree before removing it."
  fi
  exit 1
fi

binary=d2
if [ "$os" = windows ]; then binary=d2.exe; fi
bin=$prefix/bin/$binary
man=$prefix/share/man/man1/d2.1
tree=$lib/d2-$version
bin_tmp= man_tmp=
finish() {
  status=$?
  trap - EXIT
  trap '' HUP INT TERM
  # Renaming the executable is the commit point. Before it, restore the
  # release tree and manpage if their replacements were already published.
  if [ -f "$lock/publishing" ] && [ -f "$bin_tmp/$binary" ]; then
    if [ ! -d "$lock/new" ]; then rm -rf "$tree"; fi
    if [ -e "$lock/old" ] || [ -L "$lock/old" ]; then mv "$lock/old" "$tree"; fi
    if [ ! -e "$man_tmp/new" ]; then
      if [ -e "$man_tmp/old" ] || [ -L "$man_tmp/old" ]; then
        mv -f "$man_tmp/old" "$man"
      else
        rm -f "$man"
      fi
    fi
  fi
  for scratch in "$bin_tmp" "$man_tmp" "$lock"; do
    if [ -n "$scratch" ]; then rm -rf "$scratch" || echo >&2 "Could not remove $scratch"; fi
  done
  exit "$status"
}
trap finish EXIT
trap 'exit 1' HUP INT TERM

current_binary=
if [ -f "$bin" ]; then current_binary=$(cksum < "$bin"); fi
if [ "$current_binary" != "$expected_binary" ]; then
  echo >&2 "The D2 executable in $prefix changed during this operation. Rerun the installer."
  exit 1
fi

if [ "$action" = uninstall ]; then
  if [ ! -d "$lib/d2-$previous" ]; then
    echo >&2 "Missing standalone release directory $lib/d2-$previous"
    exit 1
  fi
  rm -f "$man" "$bin"
  rm -rf "$lib/d2-$previous"
  exit
fi

# Each incoming file is on its destination filesystem, even if bin, man and
# lib are separate mounts. A failed rename leaves the existing executable intact.
mkdir -p "${bin%/*}" "${man%/*}"
if [ -d "$bin" ] || [ -d "$man" ]; then
  echo >&2 "Expected a file at $bin and $man"
  exit 1
fi
bin_tmp=$(mktemp -d "${bin%/*}/.d2-install.XXXXXXXX")
man_tmp=$(mktemp -d "${man%/*}/.d2-install.XXXXXXXX")
install -m 755 "$stage/prefix/bin/$binary" "$bin_tmp/$binary"
install -m 644 "$stage/prefix/share/man/man1/d2.1" "$man_tmp/new"
cp -R "$stage/d2-$version" "$lock/new"
if [ -e "$man" ] || [ -L "$man" ]; then cp -pP "$man" "$man_tmp/old"; fi

touch "$lock/publishing"
if [ -e "$tree" ] || [ -L "$tree" ]; then mv "$tree" "$lock/old"; fi
mv "$lock/new" "$tree"
mv -f "$man_tmp/new" "$man"
mv -f "$bin_tmp/$binary" "$bin"
if [ -n "$previous" ] && [ "$previous" != "$version" ]; then
  rm -rf "$lib/d2-$previous" || echo >&2 "Installed d2-$version; could not remove $lib/d2-$previous"
fi
PUBLISH
)

uninstall_d2_brew() {
  sh_c brew remove d2
}

cache_dir() {
  if [ -n "${XDG_CACHE_HOME-}" ]; then
    echo "$XDG_CACHE_HOME/d2/release"
  elif [ -n "${HOME-}" ]; then
    echo "$HOME/.cache/d2/release"
  else
    echo "/tmp/d2-cache/release"
  fi
}

ensure_version() {
  if [ "$VERSION" = latest ]; then
    log "fetching latest version of $REPO"
    release_url="https://github.com/$REPO/releases"
    tag_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$'
    if redirect_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$release_url/latest") &&
      [ "$redirect_url" = "$release_url/tag/${redirect_url##*/}" ] &&
      printf '%s\n' "${redirect_url##*/}" | grep -Eq "$tag_pattern"; then
      VERSION=${redirect_url##*/}
    else
      warn "could not resolve the latest release via redirect, trying the GitHub API"
      api_url="https://api.github.com/repos/$REPO/releases/latest"
      if ! release_info=$(DRY_RUN= curl_gh -sS -H "'Accept: application/vnd.github+json'" "$api_url"); then
        echoerr "could not fetch the latest release from $api_url"
        return 1
      fi
      VERSION=$(printf '%s\n' "$release_info" | sed -nE 's/^.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]*)".*$/\1/p')
      if ! printf '%s\n' "$VERSION" | grep -Eq "$tag_pattern"; then
        echoerr "the GitHub API returned no valid release tag for $REPO"
        return 1
      fi
    fi
    log "detected latest version: $VERSION"
  fi

  # Ensure VERSION has 'v' prefix
  case "$VERSION" in
    v*) ;;
    *) VERSION="v$VERSION" ;;
  esac
  # The release tag is used in paths, shell commands, and the expected signing identity.
  case "$VERSION" in
    *[!A-Za-z0-9.-]*) echoerr "invalid release version"; return 1 ;;
  esac
}

curl_gh() {
  retry_curl_gh "$@"
}

retry_curl_gh() {
  local max_attempts=3
  local attempt=1
  local delay=1
  
  while [ $attempt -le $max_attempts ]; do
    set +e
    sh_c curl -fL ${GITHUB_TOKEN:+"-H \"Authorization: Bearer \$GITHUB_TOKEN\""} "$@"
    local exit_code=$?
    set -e
    
    # Success case
    if [ $exit_code -eq 0 ]; then
      return 0
    fi
    
    # Retry on HTTP errors (exit code 22) which includes 403, 429, 5xx
    if [ $exit_code -eq 22 ] && [ $attempt -lt $max_attempts ]; then
      warn "GitHub API request failed (attempt $attempt/$max_attempts), retrying in ${delay}s..."
      sleep $delay
      delay=$((delay * 2))
      attempt=$((attempt + 1))
    else
      # Log the reason for failure
      if [ $exit_code -eq 22 ]; then
        warn "GitHub API request failed after $max_attempts attempts, giving up"
      else
        warn "GitHub API request failed with non-retryable error (exit code: $exit_code)"
      fi
      return $exit_code
    fi
  done
}

fetch_gh() {
  url=$1
  file=$2
  accept=$3

  if [ -e "$file" ]; then
    log "reusing $file"
    return
  fi

  curl_gh -#o "$file.inprogress" -C- -H "'Accept: $accept'" "$url"
  sh_c mv "$file.inprogress" "$file"
}

# The main function does more than provide organization. It provides robustness in that if
# the install script was to only partial download into sh, sh will not execute it because
# main is not invoked until the very last byte.
main "$@"
