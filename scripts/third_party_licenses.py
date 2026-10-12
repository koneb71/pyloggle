#!/usr/bin/env python3
"""Write THIRD_PARTY_LICENSES.md and web/public/licenses.txt.

Lists every third-party Go module linked into cmd/ingestd and every JavaScript package the web
UI ships (npm runtime dependencies), with each license text. Identical texts are printed once.
Exits non-zero when a dependency has no license file, so a new dependency cannot slip in
unnoticed. Run from the repository root (`make licenses`); needs go and npm, nothing else.
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LICENSE_NAMES = re.compile(r"^(LICEN[CS]E|COPYING|NOTICE)([.-].*)?$", re.I)
SELF = "github.com/koneb71/pyloggle"


def spdx(text: str) -> str:
    t = " ".join(text.split()).lower()
    checks = [
        ("Apache-2.0", "apache license version 2.0"),
        ("Apache-2.0", "apache license, version 2.0"),
        ("MPL-2.0", "mozilla public license version 2.0"),
        ("MPL-2.0", "mozilla public license, version 2.0"),
        ("OFL-1.1", "sil open font license"),
        ("ISC", "permission to use, copy, modify, and/or distribute this software for any purpose with or without fee"),
        ("BSD-3-Clause", "neither the name"),
        ("BSD-2-Clause", "redistributions in binary form must reproduce"),
        ("MIT", "permission is hereby granted, free of charge"),
        ("0BSD", "permission to use, copy, modify, and/or distribute this software"),
    ]
    for ident, needle in checks:
        if needle in t:
            return ident
    return "see text"


def license_files(directory: str) -> list[tuple[str, str]]:
    out = []
    try:
        names = sorted(os.listdir(directory))
    except OSError:
        return out
    for name in names:
        path = os.path.join(directory, name)
        if LICENSE_NAMES.match(name) and os.path.isfile(path):
            with open(path, encoding="utf-8", errors="replace") as f:
                out.append((name, f.read().strip()))
    return out


OVERRIDES = os.path.join(ROOT, "scripts", "license-overrides")


def package_license_files(name: str, directory: str) -> list[tuple[str, str]]:
    """The package's own license files; else a stored override plus licenses it vendors."""
    files = license_files(directory)
    if files:
        return files
    override = os.path.join(OVERRIDES, name.replace("/", "__") + ".txt")
    if os.path.isfile(override):
        with open(override, encoding="utf-8") as f:
            files.append(("LICENSE", f.read().strip()))
    for sub in ("lib-vendor", "vendor", "third_party"):
        root = os.path.join(directory, sub)
        if os.path.isdir(root):
            for vendored in sorted(os.listdir(root)):
                files += [(f"{sub}/{vendored}/{n}", t) for n, t in license_files(os.path.join(root, vendored))]
    return files


def go_modules() -> list[dict]:
    raw = subprocess.check_output(["go", "list", "-deps", "-json", "./cmd/ingestd"], cwd=ROOT, text=True)
    mods: dict[str, dict] = {}
    for doc in re.split(r"(?m)^}\n(?={)", raw):
        if not doc.strip():
            continue
        if not doc.rstrip().endswith("}"):
            doc += "}"
        pkg = json.loads(doc)
        mod = pkg.get("Module")
        if not mod or mod.get("Main") or mod["Path"] == SELF:
            continue
        mods.setdefault(mod["Path"], {"name": mod["Path"], "version": mod.get("Version", ""), "dir": mod.get("Dir", ""), "kind": "Go module"})
    return sorted(mods.values(), key=lambda m: m["name"])


def npm_packages() -> list[dict]:
    web = os.path.join(ROOT, "web")
    res = subprocess.run(["npm", "ls", "--omit=dev", "--all", "--json", "--long"], cwd=web, capture_output=True, text=True)
    tree = json.loads(res.stdout or "{}")
    seen: dict[str, dict] = {}

    def walk(deps: dict) -> None:
        for name, info in (deps or {}).items():
            key = f"{name}@{info.get('version', '')}"
            if key not in seen and info.get("path"):
                seen[key] = {"name": name, "version": info.get("version", ""), "dir": info["path"], "kind": "npm package"}
            walk(info.get("dependencies", {}))

    walk(tree.get("dependencies", {}))
    return sorted(seen.values(), key=lambda p: (p["name"], p["version"]))


def main() -> int:
    deps = go_modules() + npm_packages()
    missing = []
    texts: dict[str, list[str]] = {}  # license text -> package labels
    rows = []
    for d in deps:
        files = package_license_files(d["name"], d["dir"])
        if not files:
            missing.append(f'{d["name"]} {d["version"]}')
            continue
        ids = sorted({spdx(t) for n, t in files if not n.upper().startswith("NOTICE")}) or ["see text"]
        label = f'{d["name"]} {d["version"]}'.strip()
        rows.append((d["kind"], d["name"], d["version"], ", ".join(ids)))
        for _, text in files:
            texts.setdefault(text, []).append(label)
    if missing:
        print("no license file found for:\n  " + "\n  ".join(missing), file=sys.stderr)
        return 1

    md = ["# Third-party licenses", "",
          "Pyloggle's `ingestd` binary and Docker image include the software below: Go modules",
          "linked into the server and JavaScript packages built into the web UI. Generated by",
          "`make licenses` (scripts/third_party_licenses.py); do not edit by hand.", "",
          "| Component | Version | License |", "|---|---|---|"]
    md += [f"| {name} | {ver} | {lic} |" for _, name, ver, lic in rows]
    md += ["", "## License texts", ""]
    txt = ["Third-party software included in Pyloggle", "=" * 41, ""]
    txt += [f"{name} {ver}: {lic}" for _, name, ver, lic in rows]
    for text, labels in sorted(texts.items(), key=lambda kv: kv[1][0]):
        md += ["### " + ", ".join(labels), "", "```text", text, "```", ""]
        txt += ["", "-" * 72, ", ".join(labels), "-" * 72, text]
    with open(os.path.join(ROOT, "THIRD_PARTY_LICENSES.md"), "w", encoding="utf-8") as f:
        f.write("\n".join(md).rstrip() + "\n")
    os.makedirs(os.path.join(ROOT, "web", "public"), exist_ok=True)
    with open(os.path.join(ROOT, "web", "public", "licenses.txt"), "w", encoding="utf-8") as f:
        f.write("\n".join(txt).rstrip() + "\n")
    print(f"{len(rows)} components, {len(texts)} distinct license texts")
    return 0


if __name__ == "__main__":
    sys.exit(main())
