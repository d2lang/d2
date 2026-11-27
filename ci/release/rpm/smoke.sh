#!/bin/sh
# Run as root in a disposable Fedora or AlmaLinux container with the RPMs mounted at /packages.
set -eu

tag=$1
binary_sha256=$2
version=${tag#v}
release=1
case "$version" in
  *-*)
    release=0.$(printf '%s' "${version#*-}" | tr '-' '.')
    version=${version%%-*}
    ;;
esac
arch=$(uname -m)
package="/packages/d2-$version-$release.$arch.rpm"

# Override the container's nodocs setting so the installation also exercises the manual and licenses.
dnf -y --disablerepo='*' --setopt=tsflags= install "$package"
test "$(rpm -q --queryformat '%{NAME}-%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}' d2)" = "d2-0:$version-$release.$arch"
rpm -V d2
test "$(command -v d2)" = /usr/bin/d2
printf '%s  /usr/bin/d2\n' "$binary_sha256" | sha256sum --check
test "$(d2 --version)" = "$tag"
test -s /usr/share/man/man1/d2.1.gz
test -s /usr/share/licenses/d2/LICENSE.txt
test -s /usr/share/licenses/d2/THIRD_PARTY_NOTICES.txt

printf 'client -> server\n' > /tmp/smoke.d2
d2 /tmp/smoke.d2 /tmp/smoke.svg
grep -q '<svg' /tmp/smoke.svg
dnf -y --disablerepo='*' remove d2
test ! -e /usr/bin/d2
test ! -e /usr/share/man/man1/d2.1.gz
