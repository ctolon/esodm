"""Validate a release tag and print its nonempty changelog section."""
import datetime
import re
import sys
from pathlib import Path

# This module has no /vN suffix: only v0 and v1 releases are valid.
TAG = re.compile(r"v(?P<version>[01]\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:alpha|beta|rc)\.[1-9][0-9]*)?)\Z")
HEADING = re.compile(r"^## \[(?P<version>[^\]]+)\](?P<suffix>[^\n]*)$", re.M)


def version_from_tag(tag):
    match = TAG.fullmatch(tag)
    if match is None:
        raise ValueError("expected v0.x.y or v1.x.y, optionally followed by -alpha.N, -beta.N or -rc.N")
    return match['version']


def section(text, version):
    headings = list(HEADING.finditer(text))
    matches = [(i, h) for i, h in enumerate(headings) if h['version'] == version]
    if len(matches) != 1:
        raise ValueError(f"CHANGELOG.md must contain exactly one section for {version}")
    index, heading = matches[0]
    suffix = heading['suffix']
    if not re.fullmatch(r" - \d{4}-\d{2}-\d{2}", suffix):
        raise ValueError("release heading must include a YYYY-MM-DD date")
    datetime.date.fromisoformat(suffix[3:])
    end = headings[index + 1].start() if index + 1 < len(headings) else len(text)
    body = text[heading.end():end].strip()
    if not body:
        raise ValueError("release notes must not be empty")
    return body + '\n'


if __name__ == '__main__':
    try:
        if len(sys.argv) != 2:
            raise ValueError('usage: release_notes.py <tag>')
        version = version_from_tag(sys.argv[1])
        changelog = Path(__file__).resolve().parents[1] / 'CHANGELOG.md'
        sys.stdout.write(section(changelog.read_text(), version))
    except ValueError as error:
        raise SystemExit(str(error)) from None
