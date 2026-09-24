#!/usr/bin/env python3
"""Verify durable recovery paths against an isolated local Compose stack.

Run after `docker compose up -d --build` and `sh scripts/seed-demo-product.sh`.
The script briefly stops services and restores them in a finally block. Use a
dedicated demo Compose project, never a shared production deployment.
"""

import json
import os
import secrets
import subprocess
import time
import urllib.error
import urllib.request


BASE = os.environ.get("SMOKE_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
STOPPED = set()


def compose(*args):
    result = subprocess.run(
        ["docker", "compose", *args], check=True, text=True, capture_output=True
    )
    return result.stdout.strip()


def stop(service):
    compose("stop", service)
    STOPPED.add(service)


def start(service):
    compose("start", service)
    STOPPED.discard(service)


def api(method, path, body=None, token=None):
    data = None if body is None else json.dumps(body).encode()
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = urllib.request.Request(BASE + path, data, headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def user():
    name = "fault_" + secrets.token_hex(8)
    password = secrets.token_urlsafe(20)
    status, _ = api(
        "POST", "/api/v1/auth/register",
        {"username": name, "password": password, "email": name + "@example.invalid"},
    )
    assert status == 200, f"registration failed: {status}"
    status, data = api("POST", "/api/v1/auth/login", {"username": name, "password": password})
    assert status == 200 and data.get("token"), "login failed"
    return data["token"]


def stock():
    return int(compose("exec", "-T", "redis", "redis-cli", "GET", "stock:1003"))


def stream_length():
    return int(compose("exec", "-T", "redis", "redis-cli", "XLEN", "seckill:orders:outbox"))


def orders(token):
    status, data = api("GET", "/api/v1/orders/my", token=token)
    assert status == 200, f"order listing failed: {status}"
    return [item for item in data.get("orders", []) if str(item.get("product_id")) == "1003"]


def wait_until(predicate, label, timeout=100):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            result = predicate()
            if result:
                return result
        except (OSError, ValueError, subprocess.CalledProcessError):
            pass
        time.sleep(2)
    raise AssertionError(f"timed out waiting for {label}")


def buy(token):
    status, data = api(
        "POST", "/api/v1/seckill/execute", {"product_id": 1003, "quantity": 1}, token
    )
    assert status == 202 and data.get("state") == "processing", f"reservation failed: {status} {data}"


def cancel(token, order_id):
    status, data = api("POST", f"/api/v1/orders/{order_id}/cancel", token=token)
    assert status == 200 and data.get("code", 0) == 0, f"cancellation failed: {status} {data}"


def main():
    assert api("GET", "/health")[0] == 200
    original_stock = stock()

    # A stopped relay leaves an accepted event in Redis. AOF must retain it
    # through Redis restart, and the relay must eventually create one order.
    stop("reservation-relay")
    first = user()
    before = stream_length()
    buy(first)
    assert stock() == original_stock - 1
    assert stream_length() == before + 1
    assert not orders(first), "order appeared while the relay was stopped"
    state_status, reservation = api("GET", "/api/v1/seckill/requests/1003", token=first)
    assert state_status == 200 and reservation.get("state") == "processing", reservation
    compose("restart", "redis")
    wait_until(lambda: compose("exec", "-T", "redis", "redis-cli", "PING") == "PONG", "Redis restart")
    assert stock() == original_stock - 1 and stream_length() == before + 1, "AOF lost accepted work"
    start("reservation-relay")
    first_order = wait_until(lambda: orders(first), "order after relay restart")[0]
    print("PASS: Redis AOF and reservation relay recovery")

    # The cancellation state is committed with a MySQL outbox row. Pausing
    # its relay leaves stock unchanged; restarting restores stock exactly once.
    stop("order-outbox-relay")
    cancel(first, first_order["id"])
    time.sleep(2)
    assert stock() == original_stock - 1, "stock restored without outbox relay"
    start("order-outbox-relay")
    wait_until(lambda: stock() == original_stock, "outbox stock restoration")
    cancel(first, first_order["id"])
    time.sleep(2)
    assert stock() == original_stock, "duplicate cancellation restored stock twice"
    print("PASS: MySQL cancellation outbox and idempotent restoration")

    # RabbitMQ can be unavailable after Redis accepted a reservation. The
    # stream relay must retain and retry the event after the broker restarts.
    stop("rabbitmq")
    second = user()
    buy(second)
    assert stock() == original_stock - 1
    time.sleep(2)
    assert not orders(second), "order appeared while RabbitMQ was stopped"
    start("rabbitmq")
    wait_until(
        lambda: (compose("exec", "-T", "rabbitmq", "rabbitmq-diagnostics", "-q", "ping"), True)[1],
        "RabbitMQ restart", timeout=120,
    )
    second_order = wait_until(lambda: orders(second), "order after RabbitMQ restart", timeout=120)[0]
    cancel(second, second_order["id"])
    wait_until(lambda: stock() == original_stock, "post-recovery cancellation")
    print("PASS: RabbitMQ outage and durable reservation retry")

    # Immutable campaign metadata lives in Redis. MySQL can be unavailable
    # during acceptance; its consumer must retain and retry the order event.
    third = user()
    stop("mysql")
    status, _ = api(
        "POST", "/api/v1/seckill/execute", {"product_id": 1003, "quantity": 1}, third
    )
    assert status == 202, f"MySQL outage did not retain a reservation: {status}"
    assert stock() == original_stock - 1, "accepted request did not reserve inventory"
    start("mysql")
    wait_until(
        lambda: (compose("exec", "-T", "mysql", "mysqladmin", "ping", "-h", "localhost", "--silent"), True)[1],
        "MySQL restart", timeout=120,
    )
    third_order = wait_until(lambda: orders(third), "order after MySQL restart", timeout=120)[0]
    cancel(third, third_order["id"])
    wait_until(lambda: stock() == original_stock, "post-MySQL cancellation")
    print("PASS: MySQL outage retains accepted work and recovers")

    # With Redis fully stopped before the request, no reservation can commit.
    fourth = user()
    before_redis_outage = stock()
    stop("redis")
    status, problem = api(
        "POST", "/api/v1/seckill/execute", {"product_id": 1003, "quantity": 1}, fourth
    )
    assert status in (503, 504) and problem.get("code") in (
        "dependency_unavailable", "outcome_unknown", "service_unavailable"
    ), f"Redis outage returned an unexpected error: {status} {problem}"
    start("redis")
    wait_until(lambda: compose("exec", "-T", "redis", "redis-cli", "PING") == "PONG", "Redis restart")
    assert stock() == before_redis_outage, "failed request changed Redis inventory"
    print("PASS: Redis outage returns a typed 5xx without reserving stock")


if __name__ == "__main__":
    try:
        main()
    finally:
        for service in sorted(STOPPED):
            try:
                start(service)
            except subprocess.CalledProcessError:
                pass
