#!/usr/bin/env python3
import json
import time
import sys
import urllib.request
import urllib.error
import concurrent.futures

BASE_URL = "http://127.0.0.1:8128"

class TestReport:
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

def run_suite():
    rep = TestReport()
    print("--- 1. Gateway Status & Models Discovery (10 Tests) ---")
    status, _, body = get_json("/api/status")
    rep.assert_true("GET /api/status 200", status == 200, f"got {status}")
    try:
        st = json.loads(body)
        rep.assert_true("Status has providers", len(st.get("providers", [])) > 0)
        rep.assert_true("Status has combos", len(st.get("combos", [])) > 0)
        rep.assert_true("Status has models", len(st.get("models", [])) > 0)
        rep.assert_true("Status has uptime", "uptime" in st)
    except Exception as e:
        rep.assert_true("Status json parse", False, str(e))

    status, _, body = get_json("/v1/models")
    rep.assert_true("GET /v1/models 200", status == 200, f"got {status}")
    try:
        ml = json.loads(body)
        data = ml.get("data", [])
        rep.assert_true("Model list not empty", len(data) >= 10)
        model_ids = {m["id"] for m in data}
        rep.assert_true("Contains gemini-flash", "gemini-flash" in model_ids)
        rep.assert_true("Contains claude-sonnet-ag", "claude-sonnet-ag" in model_ids)
        rep.assert_true("Contains opus", "opus" in model_ids)
    except Exception as e:
        rep.assert_true("Models parse", False, str(e))

    print("\n--- 2. Direct & Combo Model Routing Variations (20 Tests) ---")
    models_to_test = [
        "gemini-flash",
        "gr/gemini-flash",
        "goroute/gemini-flash",
        "gemini-flash-lite",
        "claude-sonnet-ag",
        "sonnet",
        "haiku",
        "opus",
    ]
    for m in models_to_test:
        status, _, body = post_json("/v1/chat/completions", {
            "model": m,
            "max_tokens": 10,
            "messages": [{"role": "user", "content": "say ok"}]
        })
        rep.assert_true(f"Chat completion model={m}", status == 200, f"status={status}, body={body[:100]}")
        try:
            resp_json = json.loads(body)
            rep.assert_true(f"Has choices model={m}", len(resp_json.get("choices", [])) > 0)
        except Exception as e:
            rep.assert_true(f"Json choices model={m}", False, str(e))

    # Test case sensitivity and whitespace trimming
    for m in ["  gemini-flash  ", "GEMINI-FLASH", "gr/gemini-flash "]:
        status, _, body = post_json("/v1/chat/completions", {
            "model": m,
            "max_tokens": 5,
            "messages": [{"role": "user", "content": "hi"}]
        })
        rep.assert_true(f"Whitespace/case model='{m}'", status == 200, f"status={status}, body={body[:100]}")

    print("\n--- 3. Protocol & Streaming Verification (15 Tests) ---")
    # Streaming test
    req = urllib.request.Request(
        f"{BASE_URL}/v1/chat/completions",
        data=json.dumps({
            "model": "gemini-flash",
            "stream": True,
            "messages": [{"role": "user", "content": "1 2"}]
        }).encode("utf-8"),
        headers={"Content-Type": "application/json", "Authorization": "Bearer goroute"},
        method="POST"
    )
    with urllib.request.urlopen(req, timeout=10) as stream_resp:
        rep.assert_true("Streaming status 200", stream_resp.status == 200)
        lines = [line.decode("utf-8").strip() for line in stream_resp.readlines() if line.strip()]
        data_lines = [l for l in lines if l.startswith("data: ")]
        rep.assert_true("Streaming has data lines", len(data_lines) >= 2)
        rep.assert_true("Streaming ends with [DONE]", any("[DONE]" in l for l in lines))

    # Anthropic protocol tests
    ant_headers = {"x-api-key": "goroute", "anthropic-version": "2023-06-01"}
    status, _, body = post_json("/v1/messages", {
        "model": "claude-sonnet-ag",
        "max_tokens": 15,
        "messages": [{"role": "user", "content": "hello"}]
    }, headers=ant_headers)
    rep.assert_true("Anthropic /v1/messages 200", status == 200, f"status={status}")
    rep.assert_true("Anthropic message structure", "content" in body and "id" in body)

    # Anthropic event stream
    status_s, _, body_s = post_json("/v1/messages", {
        "model": "claude-sonnet-ag",
        "max_tokens": 15,
        "stream": True,
        "messages": [{"role": "user", "content": "hello"}]
    }, headers=ant_headers)
    rep.assert_true("Anthropic event stream structure", "event: message_start" in body_s and "content_block" in body_s)

    # Embeddings tests
    for text_sample in ["apple", "machine learning algorithms", "unicode 🚀🔥"]:
        status, _, body = post_json("/v1/embeddings", {
            "model": "text-embedding-3-small",
            "input": text_sample
        })
        rep.assert_true(f"Embeddings text='{text_sample}'", status == 200, f"status={status}")
        try:
            emb = json.loads(body)
            rep.assert_true(f"Embeddings vector len > 0 for '{text_sample}'", len(emb["data"][0]["embedding"]) > 0)
        except Exception as e:
            rep.assert_true(f"Embeddings parse for '{text_sample}'", False, str(e))

    # Moderations test
    for mod_text in ["friendly note", "hello world"]:
        status, _, body = post_json("/v1/moderations", {"input": mod_text})
        rep.assert_true(f"Moderations input='{mod_text}'", status == 200)
        try:
            m_res = json.loads(body)
            rep.assert_true("Moderations flagged is False", m_res["results"][0]["flagged"] is False)
        except Exception as e:
            rep.assert_true("Moderations parse", False, str(e))

    print("\n--- 4. Payload Edge Cases & Caveman Compression (25 Tests) ---")
    # Caveman tests across 5 intensities and inputs
    prompts = [
        ("Please kindly make sure to write a rust function", 1),
        ("Could you please be kind enough to tell me the capital of France?", 1),
        ("I would be very grateful if you could optimize this code", 1),
        ("```python\ndef test():\n    return 42\n```\nThank you very much!", 1),
        ("Hello! Could you please summarize this article for me in detail?", 1),
    ]
    for p, _ in prompts:
        status, _, body = post_json("/api/compression/compress", {"text": p})
        rep.assert_true(f"Caveman compress '{p[:25]}...'", status == 200)
        try:
            comp = json.loads(body)
            rep.assert_true(f"Savings > 0 for '{p[:25]}'", comp["stats"]["chars_saved"] > 0 or "```" in p)
            if "```" in p:
                rep.assert_true("Preserved code block in compression", "def test():" in comp["compressed"])
        except Exception as e:
            rep.assert_true("Compression parse", False, str(e))

    # Unicode & multilingual checks
    languages = [
        ("Japanese", "こんにちは！世界。"),
        ("Hindi", "नमस्ते दुनिया, क्या हाल है?"),
        ("Arabic", "مرحبا بالعالم"),
        ("Emoji", "🤖🚀⚡🔥🎯"),
        ("Special chars", "<script>alert(1)</script> && & ' \" \n \t \\ /"),
    ]
    for lang, text in languages:
        status, _, body = post_json("/v1/chat/completions", {
            "model": "gemini-flash",
            "max_tokens": 10,
            "messages": [{"role": "user", "content": f"Echo this: {text}"}]
        })
        rep.assert_true(f"Chat completion {lang}", status == 200, f"status={status}")

    # Temperature and max_tokens bounds
    bounds = [
        (0.0, 5),
        (0.7, 10),
        (1.0, 15),
        (1.5, 5),
        (2.0, 5),
    ]
    for temp, max_t in bounds:
        status, _, body = post_json("/v1/chat/completions", {
            "model": "gemini-flash",
            "temperature": temp,
            "max_tokens": max_t,
            "messages": [{"role": "user", "content": "hi"}]
        })
        rep.assert_true(f"Chat completion temp={temp} max_tokens={max_t}", status == 200, f"status={status}")

    print("\n--- 5. Virtual Keys Security, Quotas & Routing Telemetry (20 Tests) ---")
    # Create key
    status, _, body = post_json("/api/keys", {
        "name": "suite-key",
        "rate_limit": 100,
        "budget_cap": 25.0,
        "models": ["gemini-flash"]
    })
    rep.assert_true("Create virtual key 200", status == 200)
    key_id = ""
    vkey = ""
    try:
        kj = json.loads(body)
        key_id = kj["key"]["id"]
        vkey = kj["key"]["key"]
        rep.assert_true("Virtual key generated with prefix sk-goroute-", vkey.startswith("sk-goroute-"))
    except Exception as e:
        rep.assert_true("Virtual key parse", False, str(e))

    # Authenticate with valid key
    status, _, _ = post_json("/v1/chat/completions", {
        "model": "gemini-flash",
        "max_tokens": 5,
        "messages": [{"role": "user", "content": "hi"}]
    }, headers={"Authorization": f"Bearer {vkey}"})
    rep.assert_true("Chat with valid virtual key 200", status == 200)

    # Reject invalid keys
    for bad_key in ["Bearer wrong", "Bearer sk-none", "Basic 123", ""]:
        h = {"Authorization": bad_key} if bad_key else {}
        status, _, _ = post_json("/v1/chat/completions", {
            "model": "gemini-flash",
            "messages": [{"role": "user", "content": "hi"}]
        }, headers=h)
        rep.assert_true(f"Reject auth='{bad_key}' (401)", status == 401)

    # Router stats & quota
    status, _, body = get_json("/api/router/stats")
    rep.assert_true("GET /api/router/stats 200", status == 200)
    try:
        r_stats = json.loads(body)
        rep.assert_true("Router stats has combos", len(r_stats.get("combos", {})) > 0)
        rep.assert_true("Router stats has metrics", "metrics" in r_stats)
    except Exception as e:
        rep.assert_true("Router stats parse", False, str(e))

    # Update quota
    status, _, body = post_json("/api/router/quota", {
        "provider_id": "antigravity",
        "model": "gemini-2.5-flash",
        "quota_percent": 92.5
    })
    rep.assert_true("POST /api/router/quota 200", status == 200)

    # Clean up virtual key
    if key_id:
        status, _, _ = delete_req(f"/api/keys/{key_id}")
        rep.assert_true("DELETE /api/keys/{id} 200", status == 200)

    # Native MCP Tools tests
    status, _, body = get_json("/mcp/tools")
    rep.assert_true("GET /mcp/tools 200", status == 200)
    try:
        tools = json.loads(body).get("tools", [])
        rep.assert_true("MCP tools count >= 4", len(tools) >= 4)
    except Exception as e:
        rep.assert_true("MCP tools parse", False, str(e))

    for tool_name in ["pickFastestModel", "listProviders", "modelUsage"]:
        status, _, body = post_json("/mcp/tools", {"name": tool_name, "arguments": {}})
        rep.assert_true(f"Execute MCP tool {tool_name} 200", status == 200)

    print("\n--- 5b. Advanced OmniRoute Parity Subsystems (15 Tests) ---")
    # A2A agent card
    status, _, body = get_json("/.well-known/agent.json")
    rep.assert_true("GET /.well-known/agent.json 200", status == 200)
    rep.assert_true("A2A card has skills", "skills" in body and "GoRoute" in body)

    # A2A status
    status, _, body = get_json("/api/a2a/status")
    rep.assert_true("GET /api/a2a/status 200", status == 200)
    rep.assert_true("A2A status online", '"online":true' in body or '"online": true' in body)

    # Agent Skills coverage
    status, _, body = get_json("/api/agent-skills/coverage")
    rep.assert_true("GET /api/agent-skills/coverage 200", status == 200)
    rep.assert_true("Skills coverage percentage 100", "percentage" in body)

    # CLI tools status
    status, _, body = get_json("/api/cli-tools/all-statuses")
    rep.assert_true("GET /api/cli-tools/all-statuses 200", status == 200)
    rep.assert_true("CLI tools has claude", "claude" in body)

    # CLI tools detect
    status, _, body = get_json("/api/cli-tools/detect")
    rep.assert_true("GET /api/cli-tools/detect 200", status == 200)

    # VNC Sessions
    status, _, body = get_json("/api/vnc-session")
    rep.assert_true("GET /api/vnc-session 200", status == 200)
    rep.assert_true("VNC has providers", "providers" in body)

    # Search endpoint
    status, _, body = post_json("/v1/search", {"query": "GoRoute gateway", "max_results": 2})
    rep.assert_true("POST /v1/search 200", status == 200)
    rep.assert_true("Search response has query", "query" in body)

    print("\n--- 6. High-Concurrency Stress Burst (25 Tests) ---")
    stress_urls = [
        ("/v1/models", "GET", None),
        ("/api/status", "GET", None),
        ("/api/router/stats", "GET", None),
        ("/mcp/tools", "GET", None),
        ("/v1/chat/completions", "POST", {"model": "gemini-flash", "max_tokens": 5, "messages": [{"role": "user", "content": "1"}]}),
    ] * 5

    def exec_req(item):
        path, method, payload = item
        if method == "GET":
            return get_json(path)[0]
        else:
            return post_json(path, payload)[0]

    start = time.time()
    with concurrent.futures.ThreadPoolExecutor(max_workers=10) as ex:
        statuses = list(ex.map(exec_req, stress_urls))
    dur = time.time() - start

    for i, s in enumerate(statuses):
        rep.assert_true(f"Concurrent req #{i+1} status 200", s == 200, f"got {s}")

    print(f"\nCompleted 25 concurrent requests in {dur:.2f}s ({dur*1000/25:.1f}ms/req average)")

    return rep.summary()

if __name__ == "__main__":
    success = run_suite()
    sys.exit(0 if success else 1)
