"""Run hermetic tests and enforce the core coverage gate; fail closed."""
import argparse
import json
from pathlib import Path
import subprocess
import time

root = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument("--staticcheck", default="staticcheck")
p.add_argument("--security", action="store_true")
args = p.parse_args()
out = root / "artifacts/verification"
out.mkdir(parents=True, exist_ok=True)
commands = [("tests", ["go", "test", "-race", "-count=1", "-coverprofile=" + str(out / "coverage.out"), "./..."]),
            ("vet", ["go", "vet", "./..."]), ("staticcheck", [args.staticcheck, "./..."])]
if args.security:
    commands.append(("security", ["govulncheck", "./..."]))
records = []
for name, command in commands:
    print("Checking", name, flush=True)
    start = time.monotonic()
    with (out / (name + ".log")).open("w") as log:
        result = subprocess.run(command, cwd=root, stdout=log, stderr=subprocess.STDOUT)
    records.append(dict(check=name, command=command, seconds=time.monotonic()-start, exit_code=result.returncode))
    (out / "results.json").write_text(json.dumps(records, indent=2))
    if result.returncode:
        print((out / (name + ".log")).read_text())
        raise SystemExit(result.returncode)
covered = total = 0
for line in (out / "coverage.out").read_text().splitlines()[1:]:
    position, statements, count = line.split()
    filename = position.rsplit(":", 1)[0]
    if filename.rsplit("/", 1)[0] == "github.com/ctolon/esodm":
        total += int(statements)
        if int(count):
            covered += int(statements)
percent = covered / total * 100 if total else 0
(out / "coverage.json").write_text(json.dumps(dict(core_percent=percent, required=90), indent=2))
print(f"Core coverage: {percent:.2f}% (required: 90%)")
if percent < 90:
    raise SystemExit(1)

package_counts = {}
file_counts = {}
for line in (out / "coverage.out").read_text().splitlines()[1:]:
    position, statements, count = line.split()
    filename = position.rsplit(":", 1)[0]
    package = filename.rsplit("/", 1)[0]
    for groups, key in ((package_counts, package), (file_counts, filename)):
        covered, total = groups.get(key, (0, 0))
        groups[key] = (covered + (int(statements) if int(count) else 0), total + int(statements))
thresholds = {"adapter/es8": 80, "adapter/es9": 80, "migrationstore": 80, "esodmtest": 80}
for package, required in thresholds.items():
    covered, total = package_counts.get("github.com/ctolon/esodm/" + package, (0, 0))
    percent = covered / total * 100 if total else 0
    print(f"{package} coverage: {percent:.2f}% (required: {required}%)")
    if percent < required:
        raise SystemExit(1)
for filename in ("migration.go", "geo.go"):
    covered, total = file_counts.get("github.com/ctolon/esodm/" + filename, (0, 0))
    percent = covered / total * 100 if total else 0
    print(f"{filename} coverage: {percent:.2f}% (required: 90%)")
    if percent < 90:
        raise SystemExit(1)
