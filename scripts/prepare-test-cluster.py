"""Configure only this project's disposable localhost test containers."""
import json
import urllib.request
import urllib.error

for port in (19280, 19290):
    base = f"http://127.0.0.1:{port}"
    def request(method, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        req = urllib.request.Request(base + path, data, {"Content-Type": "application/json"}, method=method)
        with urllib.request.urlopen(req, timeout=30) as response:
            return json.load(response)
    # Small disposable clusters share the host filesystem. Use byte watermarks,
    # preserving disk protection without requiring 5% free on a large host disk.
    request("PUT", "/_cluster/settings", {"persistent": {
        "cluster.routing.allocation.disk.watermark.low": "2gb",
        "cluster.routing.allocation.disk.watermark.high": "1gb",
        "cluster.routing.allocation.disk.watermark.flood_stage": "512mb",
    }})
    try:
        request("POST", "/_license/start_trial?acknowledge=true")
    except urllib.error.HTTPError as error:
        if error.code != 403:
            raise
    print(base, request("GET", "/_cluster/health?wait_for_status=yellow&timeout=30s")["status"])
