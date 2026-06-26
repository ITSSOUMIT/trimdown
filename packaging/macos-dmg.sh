#!/usr/bin/env bash
# Build a signed, notarized macOS .dmg containing a .pkg installer for trimdown.
#
# GoReleaser doesn't build .dmg natively, so this runs as a post-release step on
# macOS. A CLI .dmg has no drag-to-PATH target, so the dmg *contains a .pkg* that
# installs the binary to /usr/local/bin.
#
# Required env (provide via CI secrets / Apple Developer account):
#   VERSION                  e.g. 1.2.3
#   APP_CERT_ID              "Developer ID Application: NAME (TEAMID)"
#   INSTALLER_CERT_ID        "Developer ID Installer: NAME (TEAMID)"
#   AC_NOTARY_PROFILE        notarytool keychain profile name (xcrun notarytool store-credentials)
#   AMD64_BIN, ARM64_BIN     paths to the two built darwin binaries
#
# Output: dist/trimdown-${VERSION}.dmg
set -euo pipefail

: "${VERSION:?set VERSION}"
: "${AMD64_BIN:?set AMD64_BIN}"
: "${ARM64_BIN:?set ARM64_BIN}"

BUNDLE_ID="app.trimdown.cli"
WORK="$(mktemp -d)"
OUT="dist"
mkdir -p "$OUT"

# 1) Universal binary.
lipo -create -output "$WORK/trimdown" "$AMD64_BIN" "$ARM64_BIN"
chmod +x "$WORK/trimdown"

# 2) Sign the binary (hardened runtime, required for notarization).
if [[ -n "${APP_CERT_ID:-}" ]]; then
  codesign --force --options runtime --timestamp --sign "$APP_CERT_ID" "$WORK/trimdown"
fi

# 3) Build a component pkg that installs to /usr/local/bin.
ROOT="$WORK/root/usr/local/bin"
mkdir -p "$ROOT"
cp "$WORK/trimdown" "$ROOT/trimdown"
pkgbuild --root "$WORK/root" --identifier "$BUNDLE_ID" --version "$VERSION" \
  --install-location "/" "$WORK/component.pkg"

PKG="$WORK/trimdown-${VERSION}.pkg"
if [[ -n "${INSTALLER_CERT_ID:-}" ]]; then
  productbuild --package "$WORK/component.pkg" "$WORK/unsigned.pkg"
  productsign --sign "$INSTALLER_CERT_ID" "$WORK/unsigned.pkg" "$PKG"
else
  productbuild --package "$WORK/component.pkg" "$PKG"
fi

# 4) Wrap the pkg in a dmg.
DMG="$OUT/trimdown-${VERSION}.dmg"
DMGROOT="$WORK/dmgroot"
mkdir -p "$DMGROOT"
cp "$PKG" "$DMGROOT/Install trimdown.pkg"
hdiutil create -volname "trimdown ${VERSION}" -srcfolder "$DMGROOT" \
  -ov -format UDZO "$DMG"

# 5) Notarize + staple (only if credentials provided).
if [[ -n "${AC_NOTARY_PROFILE:-}" ]]; then
  xcrun notarytool submit "$DMG" --keychain-profile "$AC_NOTARY_PROFILE" --wait
  xcrun stapler staple "$DMG"
fi

echo "Built $DMG"
