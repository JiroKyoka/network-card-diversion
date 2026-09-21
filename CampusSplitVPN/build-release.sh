#!/bin/sh
set -eu

PROJECT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
GO_BIN=${GO_BIN:-go}
BUILD_DIR="$PROJECT_DIR/build/release"
DIST_DIR="$PROJECT_DIR/dist"
APP_DIR="$BUILD_DIR/校园VPN分流助手.app"

mkdir -p "$BUILD_DIR" "$DIST_DIR"

GOCACHE=${GOCACHE:-/tmp/campus-split-gocache} GOPATH=${GOPATH:-/tmp/campus-split-gopath} \
  CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 "$GO_BIN" build -trimpath -ldflags="-s -w" -o "$BUILD_DIR/macos-amd64" "$PROJECT_DIR"
GOCACHE=${GOCACHE:-/tmp/campus-split-gocache} GOPATH=${GOPATH:-/tmp/campus-split-gopath} \
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 "$GO_BIN" build -trimpath -ldflags="-s -w" -o "$BUILD_DIR/macos-arm64" "$PROJECT_DIR"
GOCACHE=${GOCACHE:-/tmp/campus-split-gocache} GOPATH=${GOPATH:-/tmp/campus-split-gopath} \
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 "$GO_BIN" build -trimpath -ldflags="-s -w -H=windowsgui" -o "$BUILD_DIR/校园VPN分流助手.exe" "$PROJECT_DIR"

mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"
lipo -create "$BUILD_DIR/macos-amd64" "$BUILD_DIR/macos-arm64" -output "$APP_DIR/Contents/MacOS/CampusSplitVPN"
cp "$PROJECT_DIR/Info.plist" "$APP_DIR/Contents/Info.plist"
cp "$PROJECT_DIR/使用说明.txt" "$APP_DIR/Contents/Resources/使用说明.txt"
chmod 755 "$APP_DIR/Contents/MacOS/CampusSplitVPN"
codesign --force --deep --sign - "$APP_DIR"

cp "$BUILD_DIR/校园VPN分流助手.exe" "$DIST_DIR/校园VPN分流助手-Windows-x64.exe"
ditto -c -k --keepParent "$APP_DIR" "$DIST_DIR/校园VPN分流助手-macOS-universal.zip"

WINDOWS_PACKAGE="$BUILD_DIR/windows-package"
mkdir -p "$WINDOWS_PACKAGE"
cp "$BUILD_DIR/校园VPN分流助手.exe" "$WINDOWS_PACKAGE/校园VPN分流助手.exe"
cp "$PROJECT_DIR/使用说明.txt" "$WINDOWS_PACKAGE/使用说明.txt"
(cd "$WINDOWS_PACKAGE" && zip -q -r "$DIST_DIR/校园VPN分流助手-Windows-x64.zip" .)

shasum -a 256 "$DIST_DIR"/*
