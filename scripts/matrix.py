"""Run the Go/Elasticsearch matrix against already running disposable services."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time

root = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument("--go", default="1.26.0,1.27.0")
p.add_argument("--race", action="store_true")
args = p.parse_args()
out = root / "artifacts/matrix"
out.mkdir(parents=True, exist_ok=True)
records = json.loads((out / "results.json").read_text()) if (out / "results.json").exists() else []
records = [r for r in records if r["go"] in ("1.26.0", "1.27.0") and r["go"] not in args.go.split(",") and r["elasticsearch"] in ("8.18.1", "8.19.7", "9.4.5", "9.5.2")]
for version in args.go.split(","):
    env = dict(os.environ, GOTOOLCHAIN="go" + version)
    for major, server, port in [(8, "8.19.7", 19280), (8, "8.18.1", 19281), (9, "9.5.2", 19290), (9, "9.4.5", 19291)]:
        command = ["go", "test", "-tags", "integration", "-timeout=5m", "-count=1", "-json"]
        if args.race:
            command.append("-race")
        command += ["./integration", "-args", "-es-url", f"http://127.0.0.1:{port}", "-es-major", str(major), "-es-licensed"]
        print(f"Go {version} / Elasticsearch {server}", flush=True)
        start = time.monotonic()
        with (out / f"go{version}-es{server}.jsonl").open("w") as log:
            result = subprocess.run(command, cwd=root, env=env, stdout=log, stderr=subprocess.STDOUT)
        records.append(dict(go=version, elasticsearch=server, command=command, seconds=time.monotonic()-start, exit_code=result.returncode))
        (out / "results.json").write_text(json.dumps(records, indent=2))
        if result.returncode:
            print((out / f"go{version}-es{server}.jsonl").read_text())
            raise SystemExit(result.returncode)
