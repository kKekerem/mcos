import re

defconfig_path = "/home/kkekerem/mcos/os/buildroot/external/configs/mcos_defconfig"
legacy_path = "/home/kkekerem/mcos/os/buildroot/buildroot/Config.in.legacy"

with open(defconfig_path) as f:
    def_lines = f.readlines()

with open(legacy_path) as f:
    legacy_text = f.read()

for line in def_lines:
    line = line.strip()
    if line and not line.startswith("#") and "=" in line:
        key = line.split("=")[0].strip()
        pattern = r"config\s+" + re.escape(key) + r"\b"
        if re.search(pattern, legacy_text):
            print(f"LEGACY SYMBOL FOUND: {key}")
