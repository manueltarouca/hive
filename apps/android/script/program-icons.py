#!/usr/bin/env python3
"""The phone's program icons, from the Mac app's own sources: one truth.

Reads apps/macos/Sources/HiveCore/{Program,Icons.generated,VendorIcons.generated}.swift
and writes programs.json: the program table (name, icon, vendor), the Simple
Icons logos with their colours and tile fills, the vendor icons (PNG, base64),
and the agents and shells sets. Run by the build (generateProgramIcons).

    program-icons.py MAC_SOURCES OUT_JSON
"""
import json
import re
import sys

src, out = sys.argv[1], sys.argv[2]
core = f"{src}/HiveCore"


def read(name):
    with open(f"{core}/{name}", encoding="utf-8") as f:
        return f.read()


program = read("Program.swift")
table = {}
for m in re.finditer(r'"([^"]+)": Program\("([^"]+)", \.(brand|symbol)\("([^"]+)"\)(?:, vendor: "([^"]+)")?\)', program):
    key, name, kind, icon, vendor = m.groups()
    table[key] = {"name": name, "icon": f"{kind}:{icon}", **({"vendor": vendor} if vendor else {})}
if len(table) < 20:
    sys.exit(f"program-icons: only {len(table)} programs parsed from Program.swift; has its format changed?")

m = re.search(r"static let fills[^=]*= \[(.*?)\]", program, re.S)
if not m:
    sys.exit("program-icons: BrandIcon.fills not found in Program.swift; has its format changed?")
fills = {k: int(v, 16) for k, v in re.findall(r'"([^"]+)": 0x([0-9A-Fa-f]{6})', m.group(1))}


def string_set(name):
    m = re.search(rf"static let {name}: Set<String> = \[(.*?)\]", program, re.S)
    if not m:
        sys.exit(f"program-icons: Programs.{name} not found in Program.swift; has its format changed?")
    return sorted(re.findall(r'"([^"]+)"', m.group(1)))


brands = {}
for m in re.finditer(r'"([^"]+)": BrandIcon\(title: "((?:[^"\\]|\\.)*)", hex: 0x([0-9A-Fa-f]{6}), path: "([^"]+)"\)', read("Icons.generated.swift")):
    slug, title, hexv, path = m.groups()
    brands[slug] = {"title": title, "hex": int(hexv, 16), "path": path, **({"fill": fills[slug]} if slug in fills else {})}

vendors = {}
for m in re.finditer(r'"([^"]+)": VendorIcon\(title: "((?:[^"\\]|\\.)*)", background: (nil|0x[0-9A-Fa-f]{6}), png: "([^"]+)"\)', read("VendorIcons.generated.swift")):
    name, title, bg, png = m.groups()
    vendors[name] = {"title": title, **({"background": int(bg, 16)} if bg != "nil" else {}), "png": png}

missing = [p["icon"] for p in table.values() if p["icon"].startswith("brand:") and p["icon"][6:] not in brands]
missing += [p["vendor"] for p in table.values() if "vendor" in p and p["vendor"] not in vendors]
if missing:
    sys.exit(f"program-icons: the table names icons the sources don't have: {missing}")

with open(out, "w", encoding="utf-8") as f:
    json.dump({"programs": table, "brands": brands, "vendors": vendors,
               "agents": string_set("agents"), "shells": string_set("shells")}, f, separators=(",", ":"))
print(f"program-icons: {len(table)} programs, {len(brands)} logos, {len(vendors)} vendor icons")
