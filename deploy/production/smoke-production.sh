#!/usr/bin/env bash
set -euo pipefail

textile_base="${TEXTILE_BASE_URL:-https://textile.vioraapps.com}"
weaving_base="${WEAVING_BASE_URL:-https://weaving.vioraapps.com}"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

curl_retry=(
  --fail
  --silent
  --show-error
  --retry 24
  --retry-delay 5
  --retry-all-errors
)

curl "${curl_retry[@]}" "$textile_base/" -o "$work_dir/home.html"
grep -Fq 'درگاه دسترسی ERP نساجی' "$work_dir/home.html"

curl "${curl_retry[@]}" "$textile_base/health" -o "$work_dir/health.json"
grep -Eq '"purchaseOrders"[[:space:]]*:[[:space:]]*"ok"' "$work_dir/health.json"
if ! grep -Eq '"telegramReports"[[:space:]]*:[[:space:]]*"ok"' "$work_dir/health.json"; then
  echo "Telegram reports are not ready yet; core Textile ERP smoke checks will continue." >&2
fi

curl "${curl_retry[@]}" "$textile_base/plans" -o "$work_dir/plans.html"
grep -Fq 'مالی' "$work_dir/plans.html"
grep -Fq 'عملیاتی' "$work_dir/plans.html"
grep -Fq 'راندمان سالن بافت' "$work_dir/plans.html"
grep -Fq 'مرکز فرمان مدیر نساجی' "$work_dir/plans.html"
grep -Fq '۳۰ روز هر سه بخش را رایگان آزمایش کنید' "$work_dir/plans.html"
grep -Fq 'شروع تست رایگان ۳۰روزه هر سه بخش' "$work_dir/plans.html"

curl "${curl_retry[@]}" -D "$work_dir/image.headers" \
  "$textile_base/assets/plans-og.png" -o "$work_dir/plans-og.png"
grep -Eiq '^content-type:[[:space:]]*image/png([[:space:]]|;|$)' "$work_dir/image.headers"
test "$(wc -c < "$work_dir/plans-og.png")" -gt 100000

curl "${curl_retry[@]}" "$textile_base/login" -o "$work_dir/login.html"
grep -Fq 'ورود به برنامه' "$work_dir/login.html"
grep -Fq 'محصولات را انتخاب و سفارش ثبت کنید' "$work_dir/login.html"

admin_status="$(curl --silent --show-error --output /dev/null \
  --dump-header "$work_dir/admin.headers" --write-out '%{http_code}' \
  "$textile_base/admin/orders")"
case "$admin_status" in
  302|303) ;;
  *)
    echo "Expected /admin/orders to redirect unauthenticated users; got HTTP $admin_status" >&2
    exit 1
    ;;
esac
grep -Eiq '^location:[[:space:]]*/admin/login([?[:space:]]|$)' "$work_dir/admin.headers"

curl "${curl_retry[@]}" "$weaving_base/api/health" -o "$work_dir/weaving-health.json"
grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"' "$work_dir/weaving-health.json"
grep -Eq '"service"[[:space:]]*:[[:space:]]*"textie-weaving-efficiency"' "$work_dir/weaving-health.json"

echo "Production smoke checks passed for Textile ERP and weaving."
