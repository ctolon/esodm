"""Run TLS/API-key integration tests against a disposable, secured Elasticsearch node."""
import argparse
import base64
import json
import os
from pathlib import Path
import secrets
import ssl
import subprocess
import time
import urllib.request
import urllib.error

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--major", type=int, choices=(8, 9), required=True)
args = parser.parse_args()
certs = root / "artifacts/secure/certs"
certs.mkdir(parents=True, exist_ok=True)
# Test-only certificate; no trust-store or production configuration is changed.
certificate = certs / "server.crt"
renew = not certificate.exists()
if not renew:
    renew = subprocess.run(["openssl", "x509", "-checkend", "86400", "-noout", "-in", str(certificate)],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode != 0
if renew:
    subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
                    "-keyout", str(certs / "server.key"), "-out", str(certs / "server.crt"),
                    "-days", "7", "-subj", "/CN=localhost", "-addext",
                    "subjectAltName=DNS:localhost,IP:127.0.0.1"], check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    # Readable by the container's unprivileged UID; this fixture key is disposable.
    (certs / "server.key").chmod(0o644)
env = os.environ.copy()
env["ESODM_TEST_PASSWORD"] = secrets.token_urlsafe(32)
service = f"es{args.major}-secure"
compose = ["docker", "compose", "-f", "compose.secure.yaml"]
url = f"https://localhost:193{args.major}0"
context = ssl.create_default_context(cafile=str(certs / "server.crt"))
started = time.monotonic()
status = 1
try:
    subprocess.run(compose + ["up", "-d", "--wait", "--force-recreate", service], cwd=root, env=env, check=True)
    auth = base64.b64encode(("elastic:" + env["ESODM_TEST_PASSWORD"]).encode()).decode()
    deadline = time.monotonic() + 90
    while True:
        ready = urllib.request.Request(url + "/_cluster/health?wait_for_status=yellow&timeout=5s",
            headers={"Authorization": "Basic " + auth})
        try:
            with urllib.request.urlopen(ready, context=context, timeout=10) as response:
                health = json.load(response)
            if not health.get("timed_out"):
                break
        except urllib.error.HTTPError as error:
            if error.code not in (401, 503):
                raise
        if time.monotonic() >= deadline:
            raise RuntimeError("secured cluster did not finish authenticated initialization")
        time.sleep(1)
    request = urllib.request.Request(url + "/_security/api_key", method="POST",
        headers={"Authorization": "Basic " + auth, "Content-Type": "application/json"},
        data=json.dumps({"name": "esodm-integration", "expiration": "1h", "role_descriptors": {
            "test-index": {"cluster": ["monitor"], "indices": [{"names": ["esodm-secure-*"],
                "privileges": ["read", "write", "create_index", "manage"]}]}}}).encode())
    with urllib.request.urlopen(request, context=context, timeout=30) as response:
        key = json.load(response)["encoded"]
    env.update(ESODM_SECURE_URL=url, ESODM_SECURE_CA=str(certs / "server.crt"),
               ESODM_SECURE_API_KEY=key, ESODM_SECURE_MAJOR=str(args.major))
    command = ["go", "test", "-p=1", "-race", "-tags=integration", "./integration", "-run=^TestSecureCluster$", "-count=1", "-timeout=3m"]
    result = subprocess.run(command, cwd=root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    status = result.returncode
    (root / f"artifacts/secure/es{args.major}.log").write_text(result.stdout)
    print(result.stdout, end="")
finally:
    if status:
        logs = subprocess.run(compose + ["logs", "--no-color", service], cwd=root, env=env,
            capture_output=True, text=True)
        (root / f"artifacts/secure/es{args.major}-cluster.log").write_text(
            logs.stdout.replace(env["ESODM_TEST_PASSWORD"], "[redacted]"))
    subprocess.run(compose + ["rm", "-s", "-f", "-v", service], cwd=root, env=env, check=True)
    (root / f"artifacts/secure/es{args.major}.json").write_text(json.dumps({
        "major": args.major, "exit_code": status, "seconds": time.monotonic() - started,
        "tls_required": True, "restricted_api_key_required": True}, indent=2))
raise SystemExit(status)
