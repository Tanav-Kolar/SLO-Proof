"""Small REST client for the project's Agent Builder and Workflow integration."""
import json
import os
from pathlib import Path
import sys
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def load_env():
    # Reuse the existing non-executing parser; never put secrets in command args.
    sys.path.insert(0, str(ROOT / 'src/agents'))
    from config import load_env as read_env
    read_env()


class APIError(RuntimeError):
    def __init__(self, status, path):
        self.status = status
        super().__init__(f'HTTP {status} from {path}; response body omitted to protect credentials')


class Client:
    def __init__(self):
        load_env()
        self.es = os.environ.get('ELASTIC_CLOUD_ENDPOINT', '').rstrip('/')
        self.kibana = os.environ.get('ELASTIC_KIBANA_ENDPOINT', '').rstrip('/')
        if not self.kibana:
            parsed = urllib.parse.urlsplit(self.es)
            if parsed.hostname and parsed.hostname.endswith('.elastic.cloud') and '.es.' in parsed.hostname:
                self.kibana = urllib.parse.urlunsplit((parsed.scheme, parsed.netloc.replace('.es.', '.kb.', 1), '', '', ''))
        self.key = os.environ.get('ELASTIC_KIBANA_API_KEY') or os.environ.get('ELASTIC_API_KEY')
        self.read_key = os.environ.get('ELASTIC_VERIFIER_API_KEY')
        if not self.kibana or not self.key:
            raise ValueError('Set ELASTIC_KIBANA_ENDPOINT and ELASTIC_KIBANA_API_KEY (or existing Elastic Cloud setup credentials)')
        for endpoint in (self.es, self.kibana):
            parsed = urllib.parse.urlsplit(endpoint)
            if parsed.scheme != 'https' or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
                raise ValueError('Elastic endpoints must be HTTPS URLs without embedded credentials')

    def request(self, method, path, body=None, *, es=False, timeout=45):
        key = self.read_key if es else self.key
        if not key:
            raise ValueError('ELASTIC_VERIFIER_API_KEY is required; no unscoped fallback for evidence reads')
        request = urllib.request.Request(
            (self.es if es else self.kibana) + path, method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={'Authorization': 'ApiKey ' + key, 'Content-Type': 'application/json', 'kbn-xsrf': 'sloproof'},
        )
        try:
            with urllib.request.urlopen(request, timeout=timeout) as response:
                raw = response.read()
                return json.loads(raw) if raw else {}
        except urllib.error.HTTPError as exc:
            raise APIError(exc.code, path) from None

    def get_optional(self, path):
        try:
            return self.request('GET', path)
        except APIError as exc:
            if exc.status == 404:
                return None
            raise
