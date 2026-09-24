#!/bin/sh
set -eu

secret_dir=${SECKILL_SECRET_DIR:-/run/seckill-secrets}
root_secret_dir=${SECKILL_MYSQL_ROOT_SECRET_DIR:-/run/mysql-root-secret}
mkdir -p "$secret_dir"
mkdir -p "$root_secret_dir"
umask 077

# Move the root credential from stacks created before root-secret isolation.
if [ -s "$secret_dir/mysql_root_password" ]; then
  if [ ! -s "$root_secret_dir/mysql_root_password" ]; then
    cp "$secret_dir/mysql_root_password" "$root_secret_dir/mysql_root_password"
  fi
  if ! cmp -s "$secret_dir/mysql_root_password" "$root_secret_dir/mysql_root_password"; then
    echo 'Conflicting persisted MySQL root credentials; refusing to start.' >&2
    exit 1
  fi
  rm "$secret_dir/mysql_root_password"
fi

make_secret() {
  secret_path="$1"
  if [ ! -s "$secret_path" ]; then
    od -An -tx1 -N32 /dev/urandom | tr -d ' \n' > "$secret_path"
  fi
  # MySQL and RabbitMQ run as separate container users and need read access.
  chmod 0444 "$secret_path"
}

make_secret "$root_secret_dir/mysql_root_password"
make_secret "$secret_dir/mysql_app_password"
make_secret "$secret_dir/rabbitmq_password"
make_secret "$secret_dir/jwt_secret"

rabbitmq_password=$(cat "$secret_dir/rabbitmq_password")
printf 'default_user = seckill\ndefault_pass = %s\n' "$rabbitmq_password" > "$secret_dir/rabbitmq.conf.tmp"
chmod 0444 "$secret_dir/rabbitmq.conf.tmp"
mv -f "$secret_dir/rabbitmq.conf.tmp" "$secret_dir/rabbitmq.conf"

echo 'Local development credentials are ready in the Docker volume.'
