from pathlib import Path
import plistlib
import struct
import sys
from zipfile import ZipFile, ZipInfo, ZIP_DEFLATED

packages = Path(sys.argv[1])
parts = [
    (0x01000007, 3, (packages / "launcher-darwin-amd64").read_bytes()),
    (0x0100000C, 0, (packages / "launcher-darwin-arm64").read_bytes()),
]
align = 1 << 14
binary = bytearray(struct.pack(">II", 0xCAFEBABE, len(parts)) + b"\0" * (20 * len(parts)))
for i, (cpu, subtype, content) in enumerate(parts):
    offset = (len(binary) + align - 1) // align * align
    struct.pack_into(">IIIII", binary, 8 + 20 * i, cpu, subtype, offset, len(content), 14)
    binary.extend(b"\0" * (offset - len(binary)))
    binary.extend(content)

info = {
    "CFBundleDevelopmentRegion": "en",
    "CFBundleExecutable": "ShippingLanesLauncher",
    "CFBundleIdentifier": "com.andyviera.shippinglanes.launcher",
    "CFBundleName": "Shipping Lanes Launcher",
    "CFBundleDisplayName": "Shipping Lanes Launcher",
    "CFBundlePackageType": "APPL",
    "CFBundleIconFile": "app_icon.icns",
    "CFBundleVersion": "4",
    "CFBundleShortVersionString": "1.2.1",
    "LSMinimumSystemVersion": "13.0",
}
with ZipFile(packages / "Shipping-Lanes-Launcher-macOS.zip", "w", ZIP_DEFLATED) as app:
    for name, value, mode in [
        ("Info.plist", plistlib.dumps(info), 0o644),
        ("Resources/app_icon.icns", Path(__file__).with_name("app_icon.icns").read_bytes(), 0o644),
        ("Resources/LICENSE-purego", Path(__file__).with_name("LICENSE-purego").read_bytes(), 0o644),
        ("MacOS/ShippingLanesLauncher", binary, 0o755),
    ]:
        entry = ZipInfo("Shipping Lanes Launcher.app/Contents/" + name)
        entry.create_system = 3
        entry.external_attr = (0o100000 | mode) << 16
        app.writestr(entry, value, ZIP_DEFLATED)
