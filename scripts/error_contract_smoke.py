#!/usr/bin/env python3
"""Exercise the public HTTP status and RFC 9457 error contract."""

import json
import os
import secrets
import urllib.error
import urllib.request


BASE = os.environ.get("SMOKE_BASE_URL", "http://127.0.0.1:8080").rstrip("/")


def request(method, path, payload=None, token=None, raw=None):
    body = raw if raw is not None else (None if payload is None else json.dumps(payload).encode())
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(BASE + path, body, headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            return response.status, response.headers, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, error.headers, json.load(error)


def problem(result, status, code):
    actual, headers, body = result
    assert actual == status, f"HTTP {actual}, want {status}: {body}"
    assert headers.get_content_type() == "application/problem+json", headers.get("Content-Type")
    assert body.get("type") == "about:blank" and body.get("status") == status
    assert body.get("code") == code and body.get("title") and body.get("detail") and body.get("instance")
    return headers, body


def main():
    name = "errors_" + secrets.token_hex(8)
    password = secrets.token_urlsafe(24)
    account = {"username": name, "password": password, "email": name + "@example.invalid"}
    registered = request("POST", "/api/v1/auth/register", account)
    assert registered[0] == 200, registered
    problem(request("POST", "/api/v1/auth/register", account), 409, "user_exists")
    problem(request("POST", "/api/v1/auth/login", {"username": name, "password": "wrong"}), 401, "invalid_credentials")
    login = request("POST", "/api/v1/auth/login", {"username": name, "password": password})
    assert login[0] == 200 and login[2].get("token"), login
    token = login[2]["token"]

    headers, _ = problem(request("GET", "/api/v1/products"), 401, "authentication_required")
    assert headers.get("WWW-Authenticate", "").startswith("Bearer")
    problem(request("POST", "/api/v1/seckill/execute", token=token, raw=b"{"), 400, "invalid_json")
    problem(request("POST", "/api/v1/seckill/execute", {"product_id": 1, "quantity": 0}, token), 422, "invalid_request")
    missing = 9_999_999_999
    problem(request("POST", "/api/v1/seckill/execute", {"product_id": missing, "quantity": 1}, token), 404, "product_not_found")
    problem(request("GET", f"/api/v1/seckill/requests/{missing}", token=token), 404, "reservation_not_found")
    problem(request("POST", "/api/v1/products", {"name": "forbidden", "price": 1, "stock": 1}, token), 403, "admin_required")
    problem(request("GET", "/api/v1/no-such-route", token=token), 404, "route_not_found")
    problem(request("POST", "/health"), 405, "method_not_allowed")
    print("PASS: HTTP 400/401/403/404/405/409/422 use problem+json with stable codes")


if __name__ == "__main__":
    main()
