"""SLOProof recovery executor.

Receives the two calls the sloproof-loop Kibana workflow makes over the
checkout-remediation-api HTTP connector:

  POST /execute   -- patch checkout-route's ordinary-traffic weights, return a
                      receipt (including a rollback token) for the workflow
                      to anchor its verification window and, if needed, the
                      rollback call to.
  POST /rollback  -- given that token, restore the weights that were in
                      place immediately before the trial.

Deliberately touches nothing else: it locates the ordinary (header-free)
rule the same way scripts/set-weights.sh does -- by shape, not by a fixed
index -- and refuses if that shape isn't exactly one rule. This mirrors
k8s/41-httproute.yaml's own comment that Rule 2 is the only rule this
executor is permitted to modify, and only its backendRef weights.

No extra dependencies: only boto3/botocore (bundled in every Lambda Python
runtime) and the standard library. The EKS bearer token is generated with
the same STS-presigned-URL scheme `aws eks get-token` / aws-iam-authenticator
use, so no kubernetes-client package or Lambda layer is needed.
"""

import base64
import json
import os
import ssl
import tempfile
import urllib.error
import urllib.request
from datetime import datetime, timezone

import boto3
import botocore.session
from botocore.signers import RequestSigner

CLUSTER_NAME = os.environ.get("CLUSTER_NAME", "sloproof")
REGION = os.environ.get("AWS_REGION", "us-west-2")
NAMESPACE = os.environ.get("NAMESPACE", "checkout")
ROUTE_NAME = os.environ.get("ROUTE_NAME", "checkout-route")

# ponytail: a static shared secret is not real authentication. It exists
# because Lambda Function URLs usable by Kibana's .webhook connector must be
# AuthType=NONE (the connector can't sign SigV4), so this is the cheapest
# thing that stops an unauthenticated internet client from moving traffic.
# Upgrade path: put API Gateway + a real authorizer in front if this becomes
# more than a demo.
SHARED_SECRET = os.environ.get("EXECUTOR_SHARED_SECRET")

_cache: dict = {}


def handler(event, context):
    try:
        if SHARED_SECRET:
            headers = {k.lower(): v for k, v in (event.get("headers") or {}).items()}
            if headers.get("x-sloproof-executor-secret") != SHARED_SECRET:
                return _response(401, {"error": "unauthorized"})

        path = event.get("rawPath") or event.get("path") or ""
        body = json.loads(event.get("body") or "{}")

        if path.endswith("/execute"):
            return _handle_execute(body)
        if path.endswith("/rollback"):
            return _handle_rollback(body)
        return _response(404, {"error": f"no route for path {path!r}"})
    except _ExecutorError as e:
        return _response(e.status, {"error": str(e)})
    except Exception as e:  # noqa: BLE001 -- last-resort guard, never leak a stack trace to the caller
        return _response(500, {"error": f"internal error: {e}"})


class _ExecutorError(Exception):
    def __init__(self, status: int, message: str):
        super().__init__(message)
        self.status = status


def _handle_execute(body: dict) -> dict:
    contract_uid = body.get("contract_uid", "unknown")
    target_route = body.get("target_route") or ROUTE_NAME
    if target_route != ROUTE_NAME:
        raise _ExecutorError(400, f"refusing to touch route {target_route!r}; only {ROUTE_NAME!r} is permitted")

    weights = body.get("weights")
    # The calling workflow step JSON-encodes this field as a string (see
    # steering_docs/sloproof-steering-doc's "API keys" section note on why
    # object-typed template values are stringified) -- accept both shapes.
    if isinstance(weights, str):
        weights = json.loads(weights)
    if not isinstance(weights, dict) or not weights:
        raise _ExecutorError(400, "weights must be a non-empty object of backend-name -> integer weight")

    before = _get_route()
    idx, rule = _find_ordinary_rule(before)
    before_weights = {b["name"]: b.get("weight", 0) for b in rule["backendRefs"]}

    new_backend_refs = [
        {"name": name, "port": 80, "weight": int(weight)} for name, weight in weights.items()
    ]
    patch = [{"op": "replace", "path": f"/spec/rules/{idx}/backendRefs", "value": new_backend_refs}]
    after = _patch_route(patch)

    applied_at = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    rollback_token = _make_rollback_token(
        route_uid=before["metadata"]["uid"],
        resource_version_before=before["metadata"]["resourceVersion"],
        weights_before=before_weights,
    )

    return _response(200, {
        "receipt": {
            "contract_uid": contract_uid,
            "route_uid": before["metadata"]["uid"],
            "applied_at": applied_at,
            "resource_version_before": before["metadata"]["resourceVersion"],
            "resource_version_after": after["metadata"]["resourceVersion"],
            "weights_before": before_weights,
            "weights_after": weights,
            "rollback_token": rollback_token,
        }
    })


def _handle_rollback(body: dict) -> dict:
    token = body.get("rollback_token")
    if not token:
        raise _ExecutorError(400, "rollback_token is required")
    state = _read_rollback_token(token)

    current = _get_route()
    idx, _rule = _find_ordinary_rule(current)
    new_backend_refs = [
        {"name": name, "port": 80, "weight": int(weight)}
        for name, weight in state["weights_before"].items()
    ]
    patch = [{"op": "replace", "path": f"/spec/rules/{idx}/backendRefs", "value": new_backend_refs}]
    after = _patch_route(patch)

    return _response(200, {
        "receipt": {
            "rolled_back_to": state["weights_before"],
            "applied_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "resource_version_after": after["metadata"]["resourceVersion"],
        }
    })


def _find_ordinary_rule(route: dict):
    rules = route["spec"]["rules"]
    candidates = [
        (i, r) for i, r in enumerate(rules)
        if not any("headers" in m for m in r.get("matches", []))
    ]
    if len(candidates) != 1:
        raise _ExecutorError(500, f"expected exactly one header-free rule on {ROUTE_NAME}, found {len(candidates)}")
    return candidates[0]


# --- rollback token: self-contained, no external state store needed -------

def _make_rollback_token(**state) -> str:
    return base64.urlsafe_b64encode(json.dumps(state).encode()).decode().rstrip("=")


def _read_rollback_token(token: str) -> dict:
    padded = token + "=" * (-len(token) % 4)
    try:
        return json.loads(base64.urlsafe_b64decode(padded))
    except Exception as e:
        raise _ExecutorError(400, f"malformed rollback_token: {e}") from e


# --- EKS API access: STS-presigned bearer token, no kubernetes-client dep -

def _get_cluster_info() -> dict:
    if "cluster" not in _cache:
        eks = boto3.client("eks", region_name=REGION)
        c = eks.describe_cluster(name=CLUSTER_NAME)["cluster"]
        ca_path = os.path.join(tempfile.gettempdir(), "eks-ca.pem")
        with open(ca_path, "wb") as f:
            f.write(base64.b64decode(c["certificateAuthority"]["data"]))
        _cache["cluster"] = {"endpoint": c["endpoint"], "ca_path": ca_path}
    return _cache["cluster"]


def _get_bearer_token() -> str:
    # Same scheme as `aws eks get-token` / aws-iam-authenticator: a
    # short-lived, SigV4-presigned STS GetCallerIdentity URL, base64'd with
    # a "k8s-aws-v1." prefix so the API server's webhook token
    # authenticator can turn it back into the caller's IAM identity.
    session = botocore.session.get_session()
    client = session.create_client("sts", region_name=REGION)
    signer = RequestSigner(
        client.meta.service_model.service_id, REGION, "sts", "v4",
        session.get_credentials(), session.get_component("event_emitter"),
    )
    params = {
        "method": "GET",
        "url": f"https://sts.{REGION}.amazonaws.com/?Action=GetCallerIdentity&Version=2011-06-15",
        "body": {},
        "headers": {"x-k8s-aws-id": CLUSTER_NAME},
        "context": {},
    }
    url = signer.generate_presigned_url(params, region_name=REGION, expires_in=60, operation_name="")
    return "k8s-aws-v1." + base64.urlsafe_b64encode(url.encode()).decode().rstrip("=")


def _k8s_request(method: str, path: str, body: bytes | None = None, content_type: str = "application/json") -> dict:
    cluster = _get_cluster_info()
    req = urllib.request.Request(cluster["endpoint"] + path, data=body, method=method, headers={
        "Authorization": f"Bearer {_get_bearer_token()}",
        "Content-Type": content_type,
        "Accept": "application/json",
    })
    ctx = ssl.create_default_context(cafile=cluster["ca_path"])
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=10) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        detail = e.read().decode(errors="replace")
        raise _ExecutorError(502, f"kubernetes API {method} {path} -> {e.code}: {detail}") from e


def _get_route() -> dict:
    path = f"/apis/gateway.networking.k8s.io/v1/namespaces/{NAMESPACE}/httproutes/{ROUTE_NAME}"
    return _k8s_request("GET", path)


def _patch_route(patch: list) -> dict:
    path = f"/apis/gateway.networking.k8s.io/v1/namespaces/{NAMESPACE}/httproutes/{ROUTE_NAME}"
    return _k8s_request("PATCH", path, body=json.dumps(patch).encode(), content_type="application/json-patch+json")


def _response(status: int, body: dict) -> dict:
    return {"statusCode": status, "headers": {"Content-Type": "application/json"}, "body": json.dumps(body)}
