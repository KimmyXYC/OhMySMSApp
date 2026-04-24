#!/usr/bin/env bash
# 生成 bcrypt 密码 hash（用于 auth.password_bcrypt 字段）
# 用法：./scripts/hash-password.sh 'your-plain-password'
# 需要：python3 + passlib 或 htpasswd
set -euo pipefail

if [ -z "${1:-}" ]; then
  echo "usage: $0 <password>"
  exit 1
fi

if command -v htpasswd >/dev/null 2>&1; then
  # apache2-utils
  htpasswd -nbBC 12 "" "$1" | cut -d: -f2
elif command -v python3 >/dev/null 2>&1 && python3 -c 'import bcrypt' 2>/dev/null; then
  python3 -c "import bcrypt,sys; print(bcrypt.hashpw(sys.argv[1].encode(), bcrypt.gensalt(12)).decode())" "$1"
else
  echo "need either 'htpasswd' (apt install apache2-utils) or 'python3-bcrypt'" >&2
  exit 2
fi
