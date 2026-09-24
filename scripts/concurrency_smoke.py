#!/usr/bin/env python3
"""Check the complete path with 40 users competing for 20 units.

Run only against a dedicated local Compose demo stack. This checks business
invariants; its timings are not a throughput benchmark.
"""

from concurrent.futures import ThreadPoolExecutor
import json
import os
import secrets
import subprocess
import time
import urllib.error
import urllib.request


BASE = os.environ.get("SMOKE_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
MYSQL = [
    "docker", "compose", "exec", "-T", "mysql", "sh", "-ec",
    'MYSQL_PWD="$(cat /run/seckill-secrets/mysql_app_password)"; export MYSQL_PWD; exec mysql -N -B -u seckill seckill_shop',
]


def run(*args, input_text=None):
    return subprocess.run(
        args, input=input_text, text=True, capture_output=True, check=True
    ).stdout.strip()


def sql(query):
    return run(*MYSQL, input_text=query + "\n")


def redis(*args):
    return run("docker", "compose", "exec", "-T", "redis", "redis-cli", *args)


def api(method, path, body=None, token=None):
    data = None if body is None else json.dumps(body).encode()
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    request = urllib.request.Request(BASE + path, data, headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, json.load(error)


def main():
    nonce = secrets.randbelow(10**9) + 10**9
    product_id = 6_000_000_000 + nonce
    start, end = int(time.time()) - 60, int(time.time()) + 3600
    users = []
    success = False
    try:
        sql(
            "INSERT INTO products (id,name,description,price,stock,image_url,seckill_start_time,seckill_end_time,created_at,updated_at) "
            f"VALUES ({product_id},'Concurrent smoke','Local fixture',9.99,20,'',FROM_UNIXTIME({start}),FROM_UNIXTIME({end}),NOW(),NOW());"
        )
        redis("SET", f"stock:{product_id}", "20")
        redis("HSET", f"seckill:campaign:{product_id}", "active", "1", "price_cents", "999", "start_unix", str(start), "end_unix", str(end))

        for i in range(40):
            name = f"concurrent_{nonce}_{i}"
            password = secrets.token_urlsafe(20)
            status, body = api("POST", "/api/v1/auth/register", {
                "username": name, "password": password, "email": name + "@example.invalid",
            })
            assert status == 200, f"registration {i}: {status} {body}"
            user_id = int(body["user"]["id"])
            status, body = api("POST", "/api/v1/auth/login", {"username": name, "password": password})
            assert status == 200 and body.get("token"), f"login {i} failed"
            users.append((user_id, body["token"]))

        def reserve(user):
            _, token = user
            return api("POST", "/api/v1/seckill/execute", {"product_id": product_id, "quantity": 1}, token)

        with ThreadPoolExecutor(max_workers=40) as pool:
            results = list(pool.map(reserve, users))
        accepted = sum(status == 202 and body.get("state") == "processing" for status, body in results)
        rejected = sum(status == 409 and body.get("code") == "sold_out" for status, body in results)
        assert (accepted, rejected) == (20, 20), f"accepted={accepted}, rejected={rejected}, responses={results}"
        assert redis("GET", f"stock:{product_id}") == "0", "Redis stock not zero"

        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            count, distinct = map(int, sql(
                f"SELECT COUNT(*),COUNT(DISTINCT user_id) FROM orders WHERE product_id={product_id};"
            ).split("\t"))
            db_stock = int(sql(f"SELECT stock FROM products WHERE id={product_id};"))
            if count == 20 and distinct == 20 and db_stock == 0:
                break
            time.sleep(2)
        else:
            raise AssertionError(f"final state: orders={count}, distinct={distinct}, MySQL stock={db_stock}")
        print("PASS: 40 HTTP requests, 20 accepted, 20 sold out, 20 unique persisted orders, stock 0")
        success = True
    finally:
        if success:
            sql(f"DELETE FROM orders WHERE product_id={product_id}; DELETE FROM products WHERE id={product_id};")
            if users:
                user_ids = ",".join(str(user_id) for user_id, _ in users)
                sql(f"DELETE FROM users WHERE id IN ({user_ids});")
            redis("DEL", f"stock:{product_id}", f"seckill:joined:product:{product_id}", f"seckill:campaign:{product_id}")
            redis("SREM", "product:dirty", str(product_id))
        else:
            print(f"Failure evidence retained for product_id={product_id}")


if __name__ == "__main__":
    main()
