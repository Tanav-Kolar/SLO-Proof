# Forge the Future Hackathon

Private team repository for the Elastic Forge the Future Hackathon.

## Start here

1. Copy `.env.example` to `.env` and add your keys locally.
2. Put application code in `src/`.
3. Put notes, architecture, and screenshots in `docs/`.
4. Fill in `SUBMISSION.md` as you build. Judges read that file first.

See the [SLOProof demo and service reuse plan](docs/DEMO_AND_SERVICE_REUSE.md) for the proposed checkout demo, metrics, recovery criteria, and extension to other APIs.

See the [service architecture](docs/SERVICE_ARCHITECTURE.md) for the verified capability baseline, plane-by-plane design, credential boundaries, and cluster-isolation requirement.

See the [checkout service spec](docs/CHECKOUT_SERVICE_SPEC.md) for the service's endpoints, fault profiles, configuration layers, and the deploy and demo scripts.

## Credentials

Do not commit AWS codes, API keys, or connection strings, even though this repository is private. Keep secrets in `.env`.

## Submit

Keep `SUBMISSION.md` current through the event. Use a pull request if more than one person is changing the same files.
