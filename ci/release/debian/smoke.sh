#!/bin/sh
# Run as root in a disposable Debian container with release packages mounted at /packages.
set -eu

tag=$1
arch=$2
version=${tag#v}
package="/packages/d2_${version}_${arch}.deb"
debian_version=$(printf '%s\n' "$version" | sed 's/-/~/')
test "$(dpkg --print-architecture)" = "$arch"

# Debian slim images normally discard manpages and other documentation.
printf '%s\n' 'path-include=/usr/share/man/*' 'path-include=/usr/share/doc/*' > /etc/dpkg/dpkg.cfg.d/zz-d2-smoke
apt-get install -y --no-install-recommends "$package"
test "$(dpkg-query -W -f='${Version} ${Architecture}' d2)" = "$debian_version $arch"
test "$(command -v d2)" = /usr/bin/d2
test "$(d2 --version)" = "$tag"
gzip -t /usr/share/man/man1/d2.1.gz /usr/share/doc/d2/THIRD_PARTY_NOTICES.txt.gz
test -s /usr/share/doc/d2/copyright

printf 'client -> server\n' > /tmp/smoke.d2
d2 /tmp/smoke.d2 /tmp/smoke.svg
grep -q '<svg' /tmp/smoke.svg
apt-get remove -y d2
test ! -e /usr/bin/d2
test ! -e /usr/share/man/man1/d2.1.gz
