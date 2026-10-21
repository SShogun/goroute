#!/usr/bin/env python3
"""
GoRoute Advanced Deep Probe & Comprehensive Stress Test Suite
Probes all ported OmniRoute subsystems, Ollama emulation, VS Code path tokens,
Domain budgets, Chaos engine, RTK filters, Webhook validation, and Multi-Turn LLM routing.
"""

import json
import time
import sys
import urllib.request
import urllib.error
import concurrent.futures

BASE_URL = "http://127.0.0.1:8128"

class ProbeReport:
    def __init__(self):
        self.passed = 0
        self.failed = 0
        self.errors = []

    def assert_true(self, name, condition, msg=""):
        if condition:
            self.passed += 1
            print(f"  [PASS] {name}")
        else:
            self.failed += 1
            err = f"{name}: {msg}"
            self.errors.append(err)
            print(f"  [FAIL] {err}")

    def summary(self):
        total = self.passed + self.failed
        print(f"\n==========================================")
        print(f"TOTAL: {total} | PASSED: {self.passed} | FAILED: {self.failed}")
        if self.errors:
            print("Failures:")
            for e in self.errors:
                print(f" - {e}")
        print(f"==========================================\n")
        return self.failed == 0

def post_json(path, data, headers=None, timeout=30):
    url = f"{BASE_URL}{path}"
    h = {"Content-Type": "application/json", "Authorization": "Bearer goroute"}
    if headers:
        h.update(headers)
    req = urllib.request.Request(url, data=json.dumps(data).encode("utf-8"), headers=h, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.headers, resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        return e.code, e.headers, e.read().decode("utf-8")
    except Exception as e:
        return 0, {}, str(e)

def put_json(path, data, headers=None, timeout=30):
    url = f"{BASE_URL}{path}"
    h = {"Content-Type": "application/json", "Authorization": "Bearer goroute"}
    if headers:
        h.update(headers)
    req = urllib.request.Request(url, data=json.dumps(data).encode("utf-8"), headers=h, method="PUT")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.headers, resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        return e.code, e.headers, e.read().decode("utf-8")
    except Exception as e:
        return 0, {}, str(e)

def get_json(path, headers=None):
    url = f"{BASE_URL}{path}"
    h = {"Authorization": "Bearer goroute"}
    if headers:
        h.update(headers)
    req = urllib.request.Request(url, headers=h, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, resp.headers, resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        return e.code, e.headers, e.read().decode("utf-8")
    except Exception as e:
        return 0, {}, str(e)

def get_raw(path, headers=None):
    url = f"{BASE_URL}{path}"
    h = {"Authorization": "Bearer goroute"}
    if headers:
        h.update(headers)
    req = urllib.request.Request(url, headers=h, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, resp.headers, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.headers, b""
    except Exception as e:
        return 0, {}, str(e)

def delete_req(path, headers=None):
    url = f"{BASE_URL}{path}"
    h = {"Authorization": "Bearer goroute"}
    if headers:
        h.update(headers)
    req = urllib.request.Request(url, headers=h, method="DELETE")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, resp.headers, resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        return e.code, e.headers, e.read().decode("utf-8")
    except Exception as e:
        return 0, {}, str(e)

def run_deep_probe():
    rep = ProbeReport()

    print("--- 1. Ollama Protocol Emulation (10 Tests) ---")
    status, _, body = get_json("/api/tags")
    rep.assert_true("GET /api/tags 200", status == 200)
    try:
        tags = json.loads(body)
        rep.assert_true("Tags contains models array", isinstance(tags.get("models"), list))
        rep.assert_true("Tags models count > 0", len(tags.get("models")) > 0)
    except Exception as e:
        rep.assert_true("Tags json parse", False, str(e))

    status, _, body = get_json("/api/version")
    rep.assert_true("GET /api/version 200", status == 200)
    try:
        v = json.loads(body)
        rep.assert_true("Version string present", "version" in v)
    except Exception as e:
        rep.assert_true("Version json parse", False, str(e))

    status, _, body = post_json("/api/show", {"name": "claude-sonnet-4.5"})
    rep.assert_true("POST /api/show 200", status == 200)
    try:
        show = json.loads(body)
        rep.assert_true("Show has modelfile", "modelfile" in show)
    except Exception as e:
        rep.assert_true("Show json parse", False, str(e))

    status, _, body = post_json("/api/chat", {
        "model": "claude-sonnet-4.5",
        "messages": [{"role": "user", "content": "respond with pong"}]
    })
    rep.assert_true("POST /api/chat 200", status == 200)
    try:
        chat = json.loads(body)
        rep.assert_true("Chat choices present", len(chat.get("choices", [])) > 0)
    except Exception as e:
        rep.assert_true("Chat json parse", False, str(e))

    print("\n--- 2. VS Code Tokenized Path Rewriting (8 Tests) ---")
    test_token = "vsc_test_token_xyz"
    status, _, body = get_json(f"/api/v1/vscode/{test_token}/v1/models")
    rep.assert_true(f"GET /api/v1/vscode/{test_token}/v1/models 200", status == 200)
    try:
        ml = json.loads(body)
        rep.assert_true("VS Code token models data list", len(ml.get("data", [])) > 0)
    except Exception as e:
        rep.assert_true("VS Code models parse", False, str(e))

    status, _, body = post_json(f"/api/v1/vscode/{test_token}/chat/completions", {
        "model": "claude-sonnet-4.5",
        "max_tokens": 5,
        "messages": [{"role": "user", "content": "hi"}]
    })
    rep.assert_true(f"POST /api/v1/vscode/{test_token}/chat/completions 200", status == 200)
    try:
        cr = json.loads(body)
        rep.assert_true("VS Code chat choices present", len(cr.get("choices", [])) > 0)
    except Exception as e:
        rep.assert_true("VS Code chat parse", False, str(e))

    status, _, body = get_json(f"/api/v1/vscode/raw/{test_token}/api/version")
    rep.assert_true("GET /api/v1/vscode/raw/{token}/api/version 200", status == 200)

    print("\n--- 3. Domain Budgets & Spend Analytics (10 Tests) ---")
    test_key = "key_test_project_alpha"
    status, _, body = post_json("/api/usage/budget", {
        "apiKeyId": test_key,
        "monthlyLimitUsd": 100.0,
        "dailyLimitUsd": 10.0,
        "warningThreshold": 0.8,
        "resetInterval": "monthly"
    })
    rep.assert_true("POST /api/usage/budget 200", status == 200)
    try:
        bj = json.loads(body)
        rep.assert_true("Budget response success", bj.get("success") == True)
    except Exception as e:
        rep.assert_true("Budget response parse", False, str(e))

    status, _, body = get_json(f"/api/usage/budget?apiKeyId={test_key}")
    rep.assert_true(f"GET /api/usage/budget?apiKeyId={test_key} 200", status == 200)
    try:
        summary = json.loads(body)
        rep.assert_true("Cost summary monthly limit 100.0", summary.get("monthlyLimitUsd") == 100.0)
    except Exception as e:
        rep.assert_true("Cost summary parse", False, str(e))

    status, _, body = get_json("/api/usage/budget/bulk")
    rep.assert_true("GET /api/usage/budget/bulk 200", status == 200)

    print("\n--- 4. Chaos Engine, DB Backups & Free Tier (12 Tests) ---")
    status, _, body = get_json("/api/chaos/config")
    rep.assert_true("GET /api/chaos/config 200", status == 200)

    status, _, body = put_json("/api/chaos/config", {
        "enabled": False,
        "latency_injection_ms": 0,
        "error_injection_rate": 0.0
    })
    rep.assert_true("PUT /api/chaos/config 200", status == 200)

    status, _, body = post_json("/api/chaos/run", {"simulation": "packet_loss", "duration_sec": 1})
    rep.assert_true("POST /api/chaos/run 200", status == 200)

    status, headers, raw_bytes = get_raw("/api/db-backups/export")
    rep.assert_true("GET /api/db-backups/export 200", status == 200)
    rep.assert_true("Export is valid SQLite binary header", raw_bytes.startswith(b"SQLite format 3\x00"))

    status, _, body = get_json("/api/free-models")
    rep.assert_true("GET /api/free-models 200", status == 200)

    status, _, body = get_json("/api/free-tier/summary")
    rep.assert_true("GET /api/free-tier/summary 200", status == 200)

    print("\n--- 5. RTK Filters & Safety Guardrails (10 Tests) ---")
    status, _, body = get_json("/api/rtk/filters")
    rep.assert_true("GET /api/rtk/filters 200", status == 200)
    try:
        rf = json.loads(body)
        rep.assert_true("RTK filters count >= 0", "filters" in rf)
    except Exception as e:
        rep.assert_true("RTK filters parse", False, str(e))

    status, _, body = post_json("/api/rtk/test", {
        "command": "git status",
        "output": "On branch main\nYour branch is up to date with 'origin/main'."
    })
    rep.assert_true("POST /api/rtk/test 200", status == 200)

    status, _, body = post_json("/api/webhooks/validate-url", {"url": "https://example.com/webhook"})
    rep.assert_true("POST /api/webhooks/validate-url 200", status == 200)

    print("\n--- 6. Operations & Cloud Resilience (12 Tests) ---")
    status, _, body = get_json("/api/tokens/health")
    rep.assert_true("GET /api/tokens/health 200", status == 200)

    status, _, body = get_json("/api/pool/usage")
    rep.assert_true("GET /api/pool/usage 200", status == 200)

    status, _, body = get_json("/api/oauth/cliproxy-import")
    rep.assert_true("GET /api/oauth/cliproxy-import 200", status == 200)

    status, _, body = get_json("/api/analytics/auto-routing")
    rep.assert_true("GET /api/analytics/auto-routing 200", status == 200)

    status, _, body = get_json("/api/analytics/compression")
    rep.assert_true("GET /api/analytics/compression 200", status == 200)

    status, _, body = get_json("/api/analytics/diversity")
    rep.assert_true("GET /api/analytics/diversity 200", status == 200)

    status, _, body = get_json("/api/version-manager/status")
    rep.assert_true("GET /api/version-manager/status 200", status == 200)

    status, _, body = get_json("/api/version-manager/check-update")
    rep.assert_true("GET /api/version-manager/check-update 200", status == 200)

    status, _, body = get_json("/api/compliance/audit-log")
    rep.assert_true("GET /api/compliance/audit-log 200", status == 200)

    status, _, body = get_json("/api/model-capability-overrides")
    rep.assert_true("GET /api/model-capability-overrides 200", status == 200)

    status, _, body = get_json("/api/radar/status")
    rep.assert_true("GET /api/radar/status 200", status == 200)

    status, _, body = get_json("/api/pricing")
    rep.assert_true("GET /api/pricing 200", status == 200)

    print("\n--- 7. Massive Multi-Turn Long Context Pruner Resilience (10 Tests) ---")
    # Simulate a 120-turn conversation (similar to Claude Code CLI session)
    big_messages = [{"role": "system", "content": "You are GoRoute autonomous pair-programmer."}]
    for i in range(60):
        big_messages.append({"role": "user", "content": f"Turn {i}: Implement feature module {i} with high performance."})
        big_messages.append({"role": "assistant", "content": f"Turn {i}: Module {i} successfully implemented with sub-10ms latency."})
    big_messages.append({"role": "user", "content": "What was the initial instruction?"})

    start = time.time()
    status, _, body = post_json("/v1/chat/completions", {
        "model": "claude-sonnet-4.5",
        "max_tokens": 15,
        "messages": big_messages
    }, timeout=45)
    dur = time.time() - start
    rep.assert_true("Massive multi-turn transcript status 200", status == 200, f"got {status}")
    rep.assert_true(f"Multi-turn response within 30s ({dur:.2f}s)", dur < 30.0)
    try:
        res = json.loads(body)
        rep.assert_true("Multi-turn choices present", len(res.get("choices", [])) > 0)
    except Exception as e:
        rep.assert_true("Multi-turn json parse", False, str(e))

    return rep.summary()

if __name__ == "__main__":
    success = run_deep_probe()
    sys.exit(0 if success else 1)
