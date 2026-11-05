#!/usr/bin/python3
import os
import re
import shutil
import stat
import subprocess
import tempfile
import urllib.request
import zipfile
from pathlib import Path

REPOSITORY = "https://github.com/Laminara/laminara.git"
BRANCH = "refs/heads/site-static"
PUBLISHED = Path("/srv/sites/laminara/published")
REQUIRED = ("web/index.html", "docs/index.html", "docs/quickstart/index.html", "docs/reference/config/index.html")
LIMIT = 64 * 1024 * 1024


def extract(archive, target):
    total = 0
    entries = archive.infolist()
    if not entries or len(entries) > 10000:
        raise ValueError("invalid entry count")
    root = entries[0].filename
    if not entries[0].is_dir() or not re.fullmatch(r"[A-Za-z0-9_-]+/", root):
        raise ValueError("invalid archive root")
    for entry in entries[1:]:
        if not entry.filename.startswith(root):
            raise ValueError("unexpected archive root")
        parts = entry.filename[len(root):].removesuffix("/").split("/")
        if parts[0] not in ("docs", "web") or any(
            not part or part.startswith(".") or "\\" in part or any(ord(c) < 32 for c in part)
            for part in parts
        ):
            raise ValueError("unexpected path in archive")
        kind = stat.S_IFMT(entry.external_attr >> 16)
        expected = stat.S_IFDIR if entry.is_dir() else stat.S_IFREG
        if kind not in (0, expected):
            raise ValueError("link or special file in archive")
        path = target.joinpath(*parts)
        if entry.is_dir():
            path.mkdir(parents=True, exist_ok=True)
            continue
        total += entry.file_size
        if total > LIMIT:
            raise ValueError("expanded site exceeds limit")
        path.parent.mkdir(parents=True, exist_ok=True)
        with archive.open(entry) as source, path.open("xb") as out:
            shutil.copyfileobj(source, out)
    for name in REQUIRED:
        if not (target / name).is_file() or (target / name).stat().st_size == 0:
            raise ValueError(f"missing or empty {name}")


def publish(sha, archive, base):
    releases = base / "releases"
    releases.mkdir(exist_ok=True)
    current = base / "current"
    target = releases / sha
    if not target.exists():
        with tempfile.TemporaryDirectory(dir=releases) as tmp:
            staged = Path(tmp) / "site"
            staged.mkdir()
            extract(archive, staged)
            staged.rename(target)
    next_link = base / "current.next"
    next_link.unlink(missing_ok=True)
    next_link.symlink_to(Path("releases") / sha)
    os.replace(next_link, current)
    previous = sorted(
        (p for p in releases.iterdir() if p.name != sha and re.fullmatch("[a-f0-9]{40}", p.name)),
        key=lambda p: p.stat().st_mtime, reverse=True
    )
    for old in previous[2:]:
        shutil.rmtree(old)


def main():
    line = subprocess.check_output(["git", "ls-remote", "--exit-code", REPOSITORY, BRANCH], text=True)
    sha, ref = line.strip().split()
    if ref != BRANCH or not re.fullmatch("[a-f0-9]{40}", sha):
        raise ValueError("unexpected remote ref")
    current = PUBLISHED / "current"
    if current.is_symlink() and current.readlink() == Path("releases") / sha:
        return
    url = f"https://codeload.github.com/Laminara/laminara/zip/{sha}"
    with tempfile.TemporaryFile() as download:
        with urllib.request.urlopen(url, timeout=30) as response:
            while chunk := response.read(65536):
                if download.tell() + len(chunk) > LIMIT:
                    raise ValueError("download exceeds limit")
                download.write(chunk)
        with zipfile.ZipFile(download) as archive:
            publish(sha, archive, PUBLISHED)
    print(f"Published {sha}", flush=True)


if __name__ == "__main__":
    main()
