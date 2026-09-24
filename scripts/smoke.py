#!/usr/bin/env python3
"""Exercise health, registration, login, and authenticated read routes."""

import json
import os
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.request


BASE_URL = os.environ.get("SMOKE_BASE_URL", "http://127.0.0.1:8080").rstrip("/")


def request(method, path, payload=None, token=None):
    body = None if payload is None else json.dumps(payload).encode("utf-8")
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(BASE_URL + path, body, headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def wait_for_gateway():
    deadline = time.monotonic() + 180
    while time.monotonic() < deadline:
        try:
            status, data = request("GET", "/health")
            if status == 200 and data.get("status") == "ok":
                return
        except (OSError, ValueError, urllib.error.HTTPError):
            pass
        time.sleep(2)
    raise RuntimeError("API Gateway did not become healthy within 180 seconds")


def create_user():
    username = "smoke_" + secrets.token_hex(8)
    password = secrets.token_urlsafe(24)
    register_payload = {
        "username": username,
        "password": password,
        "email": username + "@example.invalid",
        "phone": "",
    }
    deadline = time.monotonic() + 120
    while True:
        status, registered = request("POST", "/api/v1/auth/register", register_payload)
        if status == 200 and registered.get("user", {}).get("username") == username:
            break
        if status in (500, 503, 504) and time.monotonic() < deadline:
            time.sleep(2)
            continue
        raise AssertionError(f"registration returned an unexpected response: {status} {registered}")

    status, logged_in = request(
        "POST", "/api/v1/auth/login", {"username": username, "password": password}
    )
    token = logged_in.get("token")
    if status != 200 or not token:
        raise AssertionError("login did not return a token")
    return token


def redis_stock():
    result = subprocess.run(
        ["docker", "compose", "exec", "-T", "redis", "redis-cli", "GET", "stock:1003"],
        check=True,
        capture_output=True,
        text=True,
    )
    return int(result.stdout.strip())


def product_orders(token):
    status, body = request("GET", "/api/v1/orders/my", token=token)
    if status != 200 or not isinstance(body, dict):
        raise AssertionError("authenticated order list failed")
    return [order for order in body.get("orders", []) if str(order.get("product_id")) == "1003"]


def main():
    wait_for_gateway()
    token = create_user()

    status, products = request("GET", "/api/v1/products?page=1&page_size=10", token=token)
    if status != 200 or "products" not in products:
        raise AssertionError("authenticated product list failed")

    product_orders(token)

    if os.environ.get("SMOKE_FULL") == "1":
        initial_stock = redis_stock()
        status, accepted = request(
            "POST", "/api/v1/seckill/execute", {"product_id": 1003, "quantity": 1}, token
        )
        if status != 202 or accepted.get("state") != "processing" or not accepted.get("status_url"):
            raise AssertionError(f"reservation was not accepted: {status} {accepted}")
        status_url = accepted["status_url"]
        if redis_stock() != initial_stock - 1:
            raise AssertionError("accepted reservation did not deduct exactly one unit")

        status, duplicate = request(
            "POST", "/api/v1/seckill/execute", {"product_id": 1003, "quantity": 1}, token
        )
        if status != 409 or duplicate.get("code") != "already_participated":
            raise AssertionError(f"duplicate reservation was not rejected: {status} {duplicate}")
        if redis_stock() != initial_stock - 1:
            raise AssertionError("duplicate reservation changed inventory")

        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            state_status, reservation = request("GET", status_url, token=token)
            if state_status == 200 and reservation.get("state") == "order_created":
                break
            time.sleep(2)
        else:
            raise AssertionError("accepted reservation did not become an order within 90 seconds")
        orders = product_orders(token)
        order_id = reservation.get("order_id")
        if not order_id or len(orders) != 1:
            raise AssertionError("reservation did not create exactly one order")

        other_token = create_user()
        status, denied = request("GET", f"/api/v1/orders/{order_id}", token=other_token)
        if status != 404 or denied.get("code") != "order_not_found":
            raise AssertionError(f"another user can read the order: {status} {denied}")
        status, denied = request("GET", status_url, token=other_token)
        if status != 404 or denied.get("code") != "reservation_not_found":
            raise AssertionError(f"another user can read the reservation: {status} {denied}")

        status, canceled = request("POST", f"/api/v1/orders/{order_id}/cancel", token=token)
        if status != 200 or canceled.get("code", 0) != 0:
            raise AssertionError(f"cancellation failed: {canceled}")
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            if redis_stock() == initial_stock:
                break
            time.sleep(2)
        else:
            raise AssertionError("cancellation did not restore inventory within 90 seconds")
        status, updated = request("GET", status_url, token=token)
        if status != 200 or updated.get("order_status") != "cancelled":
            raise AssertionError(f"reservation status did not reflect cancellation: {status} {updated}")

        status, again = request("POST", f"/api/v1/orders/{order_id}/cancel", token=token)
        if status != 200 or again.get("code", 0) != 0:
            raise AssertionError(f"repeat cancellation failed: {again}")
        time.sleep(3)
        if redis_stock() != initial_stock or len(product_orders(token)) != 1:
            raise AssertionError("repeat cancellation changed inventory or order count")
        print("Full smoke passed: tracked reservation, isolated access, one order and one stock restoration")

    print("Smoke passed: health, register, login, products, and orders")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"Smoke failed: {exc}", file=sys.stderr)
        raise
