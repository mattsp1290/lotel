#!/usr/bin/env python3
"""Write the lotel-manifest.json release manifest (schema 1).

The output format is a contract: distribution/install.sh parses it with awk.
"""
import argparse
import hashlib
import json
import os
import re
import sys

TARGETS = ("linux-x86_64", "linux-aarch64", "macos-x86_64", "macos-aarch64")
NUMBER = r"(0|[1-9][0-9]*)"
TAG = re.compile(r"v%s\.%s\.%s" % (NUMBER, NUMBER, NUMBER))


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--tag", required=True)
    p.add_argument("--base-url", required=True)
    p.add_argument("--dir", required=True)
    p.add_argument("--output", required=True)
    args = p.parse_args()

    if not TAG.fullmatch(args.tag):
        sys.exit("manifest: invalid tag %r, expected vX.Y.Z" % args.tag)
    paths = {t: os.path.join(args.dir, "lotel-" + t) for t in TARGETS}
    missing = [os.path.basename(f) for f in paths.values() if not os.path.isfile(f)]
    if missing:
        sys.exit("manifest: missing binaries in %s: %s" % (args.dir, ", ".join(missing)))

    base = args.base_url.rstrip("/")
    manifest = {
        "schema": 1,
        "version": args.tag[1:],
        "tag": args.tag,
        "assets": {t: "%s/download/%s/lotel-%s" % (base, args.tag, t) for t in TARGETS},
        "sha256": {t: digest(paths[t]) for t in TARGETS},
    }
    with open(args.output, "w", encoding="utf-8", newline="\n") as f:
        f.write(json.dumps(manifest, indent=2) + "\n")


if __name__ == "__main__":
    main()
