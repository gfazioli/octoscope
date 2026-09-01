#!/usr/bin/env python3
"""Stage goreleaser's bare binaries under the names `gh` matches on.

Run after `goreleaser release`, from the repository root. Copies the
bare-binary artifacts out of dist/ into a staging directory, ready to be
uploaded to the gh-octoscope release.

Why a script and not three lines of shell: with `formats: [binary]`
goreleaser does not write the file under its upload name. It records an
artifact whose `name` is what the asset will be called and whose `path`
points back at the build output, so `dist/` on disk holds
`octoscope_linux_amd64_v1/octoscope` while the asset is to be called
`octoscope_0.31.0_linux-amd64`. The mapping only exists in
artifacts.json, and getting it wrong is not a crash — it is an extension
that installs the wrong platform's binary.
"""

import json
import os
import shutil
import sys

# The platforms .goreleaser.yaml builds, spelled the way gh looks for
# them: it selects an asset with strings.HasSuffix(name, platform+ext).
# Listed rather than derived, so dropping a target from the build fails
# here instead of silently shipping an extension that is unavailable on
# someone's machine.
REQUIRED_SUFFIXES = (
    "darwin-amd64",
    "darwin-arm64",
    "linux-amd64",
    "linux-arm64",
    "windows-amd64.exe",
)


def stage(artifacts_path: str, out_dir: str) -> list[str]:
    with open(artifacts_path, encoding="utf-8") as fh:
        artifacts = json.load(fh)

    os.makedirs(out_dir, exist_ok=True)
    staged: dict[str, str] = {}

    for art in artifacts:
        if art.get("type") != "Binary":
            continue
        name = art.get("name", "")
        # The plain build outputs are also type Binary and are named just
        # "octoscope" / "octoscope.exe". The ones we want carry the
        # version and the <os>-<arch> tail.
        match = next((s for s in REQUIRED_SUFFIXES if name.endswith(s)), None)
        if match is None:
            continue
        if match in staged:
            raise SystemExit(
                f"two artifacts claim {match}: {staged[match]} and {name}"
            )
        shutil.copy2(art["path"], os.path.join(out_dir, name))
        staged[match] = name

    missing = [s for s in REQUIRED_SUFFIXES if s not in staged]
    if missing:
        raise SystemExit(
            "no artifact for: " + ", ".join(missing) +
            "\nstaged: " + ", ".join(sorted(staged.values()))
        )
    return [staged[s] for s in REQUIRED_SUFFIXES]


def main() -> None:
    artifacts = sys.argv[1] if len(sys.argv) > 1 else "dist/artifacts.json"
    out_dir = sys.argv[2] if len(sys.argv) > 2 else "mirror"
    for name in stage(artifacts, out_dir):
        print(name)


if __name__ == "__main__":
    main()
