# Shipping Lanes Launcher

Get the Windows x64 or universal macOS launcher from the [latest launcher release](https://github.com/andymarkviera/Shipping-Lanes-Launcher/releases/latest).

The launcher runs in a desktop window. Check for and install the latest Shipping Lanes build in that window. Sign in with your own ChatGPT account in the browser when prompted, then return to the launcher to play. The game package includes its runtime. The macOS launcher is not notarized.

## Release feed

The launcher reads [the public game release feed](https://api.github.com/repos/andymarkviera/Shipping-Lanes-Releases/releases/latest). Attach standalone game builds using these asset names:

- Windows x64 — Shipping-Lanes-Windows-x64.zip containing ShippingLanes.exe and ShippingLanes.pck together with the required runtime libraries.
- Universal macOS — Shipping-Lanes-macOS.zip containing Shipping Lanes.app.

Game releases and launcher releases live in separate repositories. Keep the launcher asset filenames stable for the website's direct downloads.

## Build

The launcher is written in Go. Build the Windows executable with GOOS=windows, GOARCH=amd64, CGO_ENABLED=0 and the linker flag -H=windowsgui. For macOS, build Darwin amd64 and arm64 as launcher-darwin-amd64 and launcher-darwin-arm64, then run package-macos.py with the directory containing both executables. Keep the checked-in `app_icon.icns` beside the packaging script. The Windows build picks up `appicon_windows_amd64.syso` automatically.
