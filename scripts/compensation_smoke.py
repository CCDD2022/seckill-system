#!/usr/bin/env python3
"""Exercise manual dead-letter compensation on a dedicated local demo stack."""

import hashlib
import json
import secrets
import subprocess
import time


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


def main():
    suffix = secrets.randbelow(10**9) + 10**9
    product_id = 3_000_000_000 + suffix
    user_id = 4_000_000_000 + suffix
    message_id = f"create:{user_id}:{product_id}"
    payload = json.dumps(
        {"user_id": user_id, "product_id": product_id, "quantity": 1, "total_price": 9.99},
        separators=(",", ":"),
    )
    archive_key = hashlib.sha256(message_id.encode()).hexdigest()
    archive_id = None
    stopped = False
    try:
        # Stock 9 and a joined marker represent one accepted reservation from
        # original stock 10. The archived event stands in for a permanent
        # order creation failure whose broker delivery has already left DLQ.
        sql(
            "INSERT INTO products (id,name,description,price,stock,image_url,seckill_start_time,seckill_end_time,created_at,updated_at) "
            f"VALUES ({product_id},'Compensation test','Local fixture',9.99,10,'',NOW()-INTERVAL 1 MINUTE,NOW()+INTERVAL 1 HOUR,NOW(),NOW());"
        )
        redis("SET", f"stock:{product_id}", "9")
        redis("SADD", f"seckill:joined:product:{product_id}", str(user_id))
        archive_id = int(
            sql(
                "INSERT INTO dead_letters (delivery_key,message_id,exchange,routing_key,body,replay_count,created_at) "
                f"VALUES ('{archive_key}','{message_id}','seckill.dlx','order.create','{payload}',0,NOW()); "
                "SELECT LAST_INSERT_ID();"
            )
        )
        run("docker", "compose", "stop", "reservation-relay", "order-create-consumer")
        stopped = True
        args = (
            "docker", "compose", "run", "--rm", "--no-deps", "compensate-dead-letter",
            "--id", str(archive_id), "--confirm-stopped",
        )
        run(*args)
        assert redis("GET", f"stock:{product_id}") == "10"
        assert redis("SISMEMBER", "seckill:orders:compensated", message_id) == "1"
        assert redis("SISMEMBER", f"seckill:joined:product:{product_id}", str(user_id)) == "1"
        run(*args)
        assert redis("GET", f"stock:{product_id}") == "10", "repeat compensation changed stock"
        print("PASS: archived create failure compensated exactly once")

        run("docker", "compose", "start", "reservation-relay", "order-create-consumer")
        stopped = False
        # The operator replay command itself must refuse a compensated case.
        attempted = subprocess.run(
            ["docker", "compose", "run", "--rm", "--no-deps", "replay-dead-letter", "--id", str(archive_id)],
            capture_output=True, text=True,
        )
        assert attempted.returncode != 0 and "already compensated" in attempted.stdout + attempted.stderr, (
            "replay command did not reject a compensated archive"
        )
        time.sleep(2)
        assert sql(f"SELECT COUNT(*) FROM orders WHERE user_id={user_id} AND product_id={product_id};") == "0"
        assert redis("GET", f"stock:{product_id}") == "10"
        print("PASS: replay command refused a compensated archive")
    finally:
        if stopped:
            run("docker", "compose", "start", "reservation-relay", "order-create-consumer")
        if archive_id is not None:
            count = sql(f"SELECT COUNT(*) FROM orders WHERE user_id={user_id} AND product_id={product_id};")
            if count == "0":
                sql(f"DELETE FROM dead_letters WHERE message_id='{message_id}'; DELETE FROM products WHERE id={product_id};")
                redis("DEL", f"stock:{product_id}", f"seckill:joined:product:{product_id}")
                redis("SREM", "seckill:orders:compensated", message_id)
                redis("SREM", "product:dirty", str(product_id))


if __name__ == "__main__":
    main()
