"""Environment, credentials, and the trust boundary the agents must respect.

The reasoning plane is advisory (D8). Nothing here may acquire a path to write a
contract, a verdict, or a routing change. That is enforced in two places: the
read key is the verifier's read-only credential when one exists, and ledger.py
refuses any event name outside its advisory allowlist.

No secret is ever printed. Only which variable was chosen.
"""
import os
import pathlib
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]


def load_env() -> None:
    """Read .env into os.environ without shelling out or adding a dependency.

    Values may be quoted; nothing is expanded. Existing process env wins, so
    `BEDROCK_MODEL_ID=... agent.py ...` overrides the file.
    """
    path = REPO / ".env"
    if not path.exists():
        sys.exit("REFUSING: no .env at repo root")
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        os.environ.setdefault(key, value)


class Config:
    def __init__(self) -> None:
        load_env()
        self.es_endpoint = os.environ.get("ELASTIC_CLOUD_ENDPOINT", "").rstrip("/")
        if not self.es_endpoint:
            sys.exit("REFUSING: ELASTIC_CLOUD_ENDPOINT not set")

        # Read path. The verifier key is read-only on requests, contracts and
        # ledger, which is exactly the agent's need. Falling back to the
        # unrestricted setup key works but breaks the separation the project
        # claims, so it is announced every time rather than silently accepted.
        self.read_key = os.environ.get("ELASTIC_VERIFIER_API_KEY", "")
        self.read_key_source = "ELASTIC_VERIFIER_API_KEY"
        if not self.read_key:
            self.read_key = os.environ.get("ELASTIC_API_KEY", "")
            self.read_key_source = "ELASTIC_API_KEY (UNSCOPED)"

        # Write path, used only for advisory and narrative ledger records.
        self.write_key = os.environ.get("ELASTIC_CONTROLLER_API_KEY", "")
        self.write_key_source = "ELASTIC_CONTROLLER_API_KEY"
        if not self.write_key:
            self.write_key = os.environ.get("ELASTIC_API_KEY", "")
            self.write_key_source = "ELASTIC_API_KEY (UNSCOPED)"

        if not self.read_key:
            sys.exit("REFUSING: no Elasticsearch key in .env")

        self.region = os.environ.get("AWS_REGION") or os.environ.get("AWS_DEFAULT_REGION") or "us-west-2"

        # Default model is deliberately not a Claude id. The Anthropic models on
        # Bedrock are not subscribed in the account whose credentials are in
        # .env -- Converse returns AccessDeniedException pointing at missing
        # aws-marketplace:Subscribe. Nova Pro is invokable there today and
        # supports Converse tool use, which the briefer needs.
        self.model_id = os.environ.get("BEDROCK_MODEL_ID", "us.amazon.nova-pro-v1:0")

    def warn_unscoped(self, stream=sys.stderr) -> None:
        if "UNSCOPED" in self.read_key_source:
            print(
                "WARNING: reading with the unrestricted setup key. The advisory is\n"
                "         still advisory, but the credential separation in the\n"
                "         architecture is not in force. Run elastic/create-api-keys.sh.",
                file=stream,
            )
