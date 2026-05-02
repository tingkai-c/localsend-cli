# Maintainer: tingkai-c
# Upstream: meowrain <meowrain@126.com>
# Contributor: ilius <saeedgnu@riseup.net>

pkgname=localsend-cli
pkgver=1.3.2
pkgrel=1
pkgdesc="CLI implementation of LocalSend protocol in Go (HTTPS-enabled fork)"
arch=('x86_64' 'aarch64' 'armv7h' 'riscv64')
url="https://github.com/tingkai-c/localsend-cli"
license=('MIT')
depends=('glibc')
makedepends=('go')

source=("$pkgname-$pkgver.tar.gz::$url/archive/refs/tags/v$pkgver.tar.gz")
sha256sums=('e47a21dc4ecae381731cd8da8e61945fd5a3814f764ba77fb3a62e84b9f278f6')

build() {
  cd "$pkgname-$pkgver"
  go build -o "$pkgname" .
}

package() {
  cd "$pkgname-$pkgver"
  install -Dm755 "$pkgname" "$pkgdir/usr/bin/$pkgname"
  install -Dm644 LICENSE "$pkgdir/usr/share/licenses/$pkgname/LICENSE"
}
