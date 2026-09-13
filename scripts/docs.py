"""Check local Markdown links and synchronize source-backed code examples."""
import argparse
import re
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]
EXAMPLE = re.compile(r"<!-- source: ([^\n]+) -->\n```(\w+)\n(.*?)\n```", re.S)


def anchors(text):
    """Return GitHub-style heading IDs, including duplicate heading suffixes."""
    text = re.sub(r"```.*?```|~~~.*?~~~", "", text, flags=re.S)
    result = set()
    for heading in re.findall(r"^#{1,6} +(.+?) *#* *$", text, re.M):
        heading = re.sub(r"<[^>]*>", "", heading)
        heading = re.sub(r"\[([^\]]+)\]\([^)]+\)", r"\1", heading)
        slug = re.sub(r"[^\w\- ]", "", heading.lower()).replace(" ", "-")
        candidate, suffix = slug, 0
        while candidate in result:
            suffix += 1
            candidate = f"{slug}-{suffix}"
        result.add(candidate)
    result.update(re.findall(r'<(?:a|h[1-6])[^>]*\bid=["\']([^"\']+)["\']', text))
    return result


def local_link_error(path, target):
    if re.match(r"[a-z]+://|mailto:", target):
        return None
    local, _, fragment = unquote(target).partition("#")
    destination = path.parent / local if local else path
    if not destination.exists():
        return f"broken link {target}"
    if "extra_docs" in Path(local).parts:
        return "release documentation links to ignored archive"
    if fragment and destination.suffix == ".md" and fragment not in anchors(destination.read_text()):
        return f"missing anchor {target}"
    return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true", help="refresh source-backed examples")
    args = parser.parse_args()
    errors = []
    documents = sorted(ROOT.glob("*.md")) + sorted((ROOT / "docs").rglob("*.md"))
    for path in documents:
        text = path.read_text()
        def replace(match):
            source = ROOT / match[1]
            if not source.is_file():
                errors.append(f"{path.relative_to(ROOT)}: missing example {match[1]}")
                return match[0]
            expected = source.read_text().rstrip()
            if match[3] != expected and not args.write:
                errors.append(f"{path.relative_to(ROOT)}: stale example {match[1]}")
            return f"<!-- source: {match[1]} -->\n```{match[2]}\n{expected}\n```"
        updated = EXAMPLE.sub(replace, text)
        if args.write and updated != text:
            path.write_text(updated)
        prose = re.sub(r"```.*?```", "", updated, flags=re.S)
        prose = re.sub(r"`[^`]*`", "", prose)
        for target in re.findall(r"\[[^\]]*\]\(([^)]+)\)", prose):
            error = local_link_error(path, target)
            if error:
                errors.append(f"{path.relative_to(ROOT)}: {error}")
    for folder in (ROOT, ROOT / "adapter", ROOT / "migrationstore", ROOT / "observe"):
        sources = folder.glob("*.go") if folder == ROOT else folder.rglob("*.go")
        for path in sources:
            for line in path.read_text().splitlines():
                if line.lstrip().startswith("//") and re.search(r"(?:docs/|extra_docs/|\b\S+\.md\b)", line):
                    errors.append(f"{path.relative_to(ROOT)}: source comment refers to documentation file")
    if errors:
        raise SystemExit("\n".join(errors))
    print(f"Documentation checks passed ({len(documents)} Markdown files).")


if __name__ == "__main__":
    main()
