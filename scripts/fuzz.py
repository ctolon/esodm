import argparse
import json
from pathlib import Path
import re
import subprocess
import time

root = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument("--duration", default="30s")
p.add_argument("--parallel", type=int, default=2)
p.add_argument("--target", default="")
args = p.parse_args()
targets = sorted(set(re.findall(r"func (Fuzz\w+)\(", (root / "fuzz_test.go").read_text())))
if args.target:
    if args.target not in targets:
        p.error("unknown target")
    targets = [args.target]
out = root / "artifacts/fuzz"
out.mkdir(parents=True, exist_ok=True)
records = json.loads((out / "results.json").read_text()) if (out / "results.json").exists() else []
records = [record for record in records if record["target"] not in targets]
for target in targets:
    command = ["go", "test", "-timeout=0", "-run=^$", "-fuzz=^" + target + "$", "-fuzztime=" + args.duration, "-parallel=" + str(args.parallel), "."]
    print("Fuzzing", target, args.duration, flush=True)
    start = time.monotonic()
    with (out / (target + ".log")).open("w") as log:
        result = subprocess.run(command, cwd=root, stdout=log, stderr=subprocess.STDOUT)
    records.append(dict(target=target, command=command, seconds=time.monotonic() - start, exit_code=result.returncode))
    (out / "results.json").write_text(json.dumps(records, indent=2))
    if result.returncode:
        raise SystemExit(result.returncode)
