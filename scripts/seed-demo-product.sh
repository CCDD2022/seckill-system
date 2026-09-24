#!/bin/sh
# Seed only an unused demo product. Never reconstruct Redis stock from an old DB row.
set -eu

mysql_query() {
  docker compose exec -T mysql sh -ec '
    MYSQL_PWD="$(cat /run/seckill-secrets/mysql_app_password)"
    export MYSQL_PWD
    exec mysql -N -B -u seckill seckill_shop
  '
}

product_exists=$(printf 'SELECT COUNT(*) FROM products WHERE id = 1003;\n' | mysql_query)
redis_exists=$(docker compose exec -T redis redis-cli EXISTS stock:1003)

case "$product_exists:$redis_exists" in
  1:1)
    echo 'Demo product 1003 already exists; inventory was left unchanged.'
    exit 0
    ;;
  1:0)
    echo 'Product 1003 exists but Redis stock is missing; refusing to restore from potentially stale MySQL stock.' >&2
    exit 1
    ;;
  0:1)
    echo 'Redis stock:1003 exists without a product row; refusing to overwrite existing inventory.' >&2
    exit 1
    ;;
  0:0) ;;
  *)
    echo "Unexpected product/Redis state: $product_exists:$redis_exists" >&2
    exit 1
    ;;
esac

mysql_query <<'SQL'
INSERT INTO products
  (id, name, description, price, stock, image_url, seckill_start_time, seckill_end_time, created_at, updated_at)
VALUES
  (1003, 'Demo Flash Sale', 'Local test fixture', 99.00, 1000, '',
   NOW() - INTERVAL 1 MINUTE, NOW() + INTERVAL 1 DAY, NOW(), NOW());
SQL

created=$(docker compose exec -T redis redis-cli SET stock:1003 1000 NX)
if [ "$created" != OK ]; then
  echo 'Product row was created, but Redis inventory was not initialized. Resolve manually; the script will not restore it.' >&2
  exit 1
fi
echo 'Created demo product 1003 with 1000 units of Redis inventory.'
