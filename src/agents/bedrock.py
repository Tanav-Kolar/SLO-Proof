"""Bedrock Converse, plus the tool-use loop the briefer needs.

This is the Investigator fallback named in SERVICE_ARCHITECTURE.md section 5,
promoted to the primary path. Converse is used rather than InvokeModel because
it gives one message shape and one toolConfig shape across model families, so
swapping Nova Pro for Llama or for a Claude id later changes one string.

Credentials come from the ambient AWS environment, which is what makes this path
work where the Kibana .bedrock connector does not: the connector form has no
session-token field, and the credentials in .env are temporary STS credentials
(ASIA...) that are invalid without one.
"""
import json
import re

import boto3
from botocore.config import Config as BotoConfig
from botocore.exceptions import ClientError


class Bedrock:
    def __init__(self, cfg):
        self.model_id = cfg.model_id
        self.client = boto3.client(
            "bedrock-runtime",
            region_name=cfg.region,
            config=BotoConfig(retries={"max_attempts": 3, "mode": "standard"},
                              read_timeout=120),
        )

    def converse(self, *, system: str, messages: list, tools: list | None = None,
                 max_tokens: int = 1200, temperature: float = 0.0) -> dict:
        kwargs = {
            "modelId": self.model_id,
            "system": [{"text": system}],
            "messages": messages,
            "inferenceConfig": {"maxTokens": max_tokens, "temperature": temperature},
        }
        if tools:
            kwargs["toolConfig"] = {"tools": tools}
        try:
            return self.client.converse(**kwargs)
        except ClientError as exc:
            code = exc.response.get("Error", {}).get("Code", "")
            if code == "AccessDeniedException":
                raise RuntimeError(
                    f"Bedrock denied {self.model_id}. The account whose credentials are "
                    f"in .env has no subscription for it. Run `agent.py preflight` to see "
                    f"which model ids are actually invokable, then set BEDROCK_MODEL_ID."
                ) from None
            raise

    @staticmethod
    def text_of(response: dict) -> str:
        text = "".join(
            block["text"]
            for block in response["output"]["message"]["content"]
            if "text" in block
        )
        # Nova emits <thinking> blocks inline in the text, not as a separate
        # content type, and they survive into anything printed on stage. Strip
        # them rather than asking the model not to -- it does it anyway.
        return re.sub(r"<thinking>.*?</thinking>", "", text, flags=re.S).strip()

    def tool_loop(self, *, system: str, question: str, tools: list, dispatch,
                  max_rounds: int = 5) -> tuple[str, list[str]]:
        """Run Converse until the model stops asking for tools.

        `dispatch(name, params)` must return (payload_for_model, query_id). The
        query_ids are collected and returned so the caller can show what the
        answer actually rests on. max_rounds is a hard stop: a model that keeps
        calling tools is failing, and an unbounded loop in a live demo is worse
        than a short answer.
        """
        messages = [{"role": "user", "content": [{"text": question}]}]
        cited: list[str] = []

        for _ in range(max_rounds):
            response = self.converse(system=system, messages=messages, tools=tools)
            content = response["output"]["message"]["content"]
            messages.append({"role": "assistant", "content": content})

            uses = [block["toolUse"] for block in content if "toolUse" in block]
            if response.get("stopReason") != "tool_use" or not uses:
                return self.text_of(response), cited

            results = []
            for use in uses:
                try:
                    payload, query_id = dispatch(use["name"], use["input"])
                    if query_id and query_id not in cited:
                        cited.append(query_id)
                    results.append({"toolResult": {
                        "toolUseId": use["toolUseId"],
                        "content": [{"json": payload}],
                    }})
                except Exception as exc:  # surfaced to the model, not swallowed
                    results.append({"toolResult": {
                        "toolUseId": use["toolUseId"],
                        "content": [{"text": f"tool error: {exc}"}],
                        "status": "error",
                    }})
            messages.append({"role": "user", "content": results})

        return ("Stopped after the tool-call limit without reaching an answer. "
                "Treat this as no answer, not as a negative finding."), cited


def extract_json(text: str) -> dict:
    """Pull the first JSON object out of a model response.

    Models wrap JSON in prose or fences even when told not to. Parsing the whole
    string first keeps the clean case cheap; the brace scan handles the rest.
    """
    text = text.strip()
    if text.startswith("```"):
        text = text.split("```")[1]
        if text.startswith("json"):
            text = text[4:]
        text = text.strip()
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        pass
    depth, start = 0, None
    for i, ch in enumerate(text):
        if ch == "{":
            if depth == 0:
                start = i
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0 and start is not None:
                try:
                    return json.loads(text[start:i + 1])
                except json.JSONDecodeError:
                    start = None
    raise ValueError(f"no JSON object in model response: {text[:300]}")
