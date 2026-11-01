# Stamps each build with a version the server can tell apart, and leaves it next
# to the firmware for tools/publish-firmware.sh.
import datetime
import os
import subprocess

Import("env")

describe = subprocess.run(
    ["git", "describe", "--tags", "--always", "--dirty"], capture_output=True, text=True
).stdout.strip()
version = f"{describe or 'unknown'}+{datetime.datetime.now():%m%d%H%M%S}"

env.Append(CPPDEFINES=[("FW_VERSION", env.StringifyMacro(version))])

build_dir = env.subst("$BUILD_DIR")
os.makedirs(build_dir, exist_ok=True)
with open(os.path.join(build_dir, "version.txt"), "w") as out:
    out.write(version)
