"""Source-controlled CPU reservation change for the three demo counter workers.

Run on raptor-ecs-ax only, after reviewing the AX resize plan. This changes
only the counter WorkerPool request; AX worker limits and memory stay intact.
"""

import json
import socket
import subprocess
import sys


NAMESPACE = "ate-demo-counter"
NAME = "counter"


def kubectl(*args):
    return subprocess.check_output(["kubectl", *args], text=True)


def current():
    value = json.loads(kubectl("get", "workerpool", NAME, "-n", NAMESPACE, "-o", "json"))
    spec = value["spec"]
    resources = spec["template"]["resources"]
    if (
        value["metadata"]["name"] != NAME
        or value["metadata"]["namespace"] != NAMESPACE
        or spec["replicas"] != 3
        or spec["sandboxClass"] != "gvisor"
        or resources["requests"]["memory"] != "1Gi"
        or resources["limits"] != {"cpu": "1", "memory": "1Gi"}
        or not spec["workerImage"].startswith("localhost:5001/ateom-gvisor@sha256:")
    ):
        raise ValueError("Counter WorkerPool differs from the reviewed POC")
    return resources["requests"]["cpu"]


def main():
    if socket.gethostname() != "raptor-ecs-ax":
        raise ValueError("Wrong host")
    mode = sys.argv[1]
    if mode not in {"check", "apply", "rollback"}:
        raise ValueError("Use check, apply or rollback")
    before = current()
    if mode == "check":
        print(f"Counter CPU request: {before}; three replicas")
        return
    expected, target = ("250m", "100m") if mode == "apply" else ("100m", "250m")
    if before != expected:
        raise ValueError("Unexpected current counter CPU request")
    patch = json.dumps([
        {"op": "test", "path": "/spec/template/resources/requests/cpu", "value": expected},
        {"op": "replace", "path": "/spec/template/resources/requests/cpu", "value": target},
    ])
    subprocess.run([
        "kubectl", "patch", "workerpool", NAME, "-n", NAMESPACE,
        "--type=json", "-p", patch, "--dry-run=server", "-o", "name",
    ], check=True, stdout=subprocess.DEVNULL)
    subprocess.run([
        "kubectl", "patch", "workerpool", NAME, "-n", NAMESPACE,
        "--type=json", "-p", patch,
    ], check=True)
    subprocess.run([
        "kubectl", "rollout", "status", "deployment/counter", "-n", NAMESPACE,
        "--timeout=180s",
    ], check=True)
    if current() != target:
        raise ValueError("Counter request did not converge")
    print(f"Counter CPU request: {before} -> {target}; three replicas ready")


if __name__ == "__main__":
    main()
