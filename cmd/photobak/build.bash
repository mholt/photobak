#!/usr/bin/env bash
set -ex

# This script builds photobak for most common platforms.

export CGO_ENABLED=0

mkdir -p builds

GOOS=linux   GOARCH=386   go build -o builds/photobak_linux_386
GOOS=linux   GOARCH=amd64 go build -o builds/photobak_linux_amd64
GOOS=linux   GOARCH=arm   go build -o builds/photobak_linux_arm7
GOOS=darwin  GOARCH=amd64 go build -o builds/photobak_mac_amd64
GOOS=windows GOARCH=386   go build -o builds/photobak_windows_386.exe
GOOS=windows GOARCH=amd64 go build -o builds/photobak_windows_amd64.exe
