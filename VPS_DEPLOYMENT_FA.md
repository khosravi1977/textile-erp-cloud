# استقرار Textile ERP روی همان VPS بدون تداخل

این پروژه برای اجرا کنار پروژه‌های دیگر شما روی همان سرور آماده شده است.

## دامنه پیشنهادی

- `textile.example.com`

## پورت‌های داخلی رزروشده روی VPS

- Portal: `127.0.0.1:8180`
- Financial frontend: `127.0.0.1:8173`
- Financial API: `127.0.0.1:8181`
- Operational app: `127.0.0.1:8191`
- PostgreSQL: `127.0.0.1:5435`
- PostgreSQL replica: `127.0.0.1:5436`
- Redis: `127.0.0.1:6381`
- Grafana: `127.0.0.1:3300`
- Prometheus: `127.0.0.1:9190`

این چیدمان با پروژه‌های `cooler-store` و `start up` تداخل ندارد.

## فایل‌های آماده

- [deploy/docker-compose.vps.yml](F:/project/textile_erp_clean/deploy/docker-compose.vps.yml)
- [deploy/nginx.textile-erp.conf](F:/project/textile_erp_clean/deploy/nginx.textile-erp.conf)
- [deploy/.env.vps.example](F:/project/textile_erp_clean/deploy/.env.vps.example)
- [deploy/vps-deploy.sh](F:/project/textile_erp_clean/deploy/vps-deploy.sh)
- [deploy/vps-nginx.sh](F:/project/textile_erp_clean/deploy/vps-nginx.sh)

## استقرار روی سرور

```bash
sudo mkdir -p /opt/textile-erp
```

کل پروژه را در مسیر زیر قرار دهید:

```text
/opt/textile-erp
```

سپس:

```bash
cd /opt/textile-erp
cp deploy/.env.vps.example .env
nano .env
sudo bash deploy/vps-deploy.sh textile.example.com
```

## SSL

پس از تنظیم DNS:

```bash
sudo certbot --nginx -d textile.example.com
```

## سلامت سرویس

- Portal health: `http://127.0.0.1:8180/health`
- Financial API health: `http://127.0.0.1:8181/health`
- Operational API health: `http://127.0.0.1:8191/api/health`

## پنل مدیریت دسترسی مشتریان

در نسخه جدید Portal، یک پنل مدیریتی برای ساخت دسترسی تست مشتریان اضافه شده است:

- آدرس پنل: `/admin`
- قابلیت‌ها:
  - ساخت یوزر و رمز برای هر پروژه
  - تعیین تاریخ انقضا یا تعداد روز تست
  - تولید لینک اختصاصی برای مشتری
  - هدایت مشتری به `Textile ERP` یا `Cooler Store`

متغیرهای محیطی مرتبط:

- `PORTAL_PUBLIC_BASE`
- `PORTAL_COOLER_STORE_URL`
- `PORTAL_ADMIN_USERNAME`
- `PORTAL_ADMIN_PASSWORD`
- `PORTAL_SESSION_SECRET`

## توضیح تصمیم فنی

برای نسخه فعلی، چندشرکتی بودن به‌صورت «مدیریت دسترسی و لینک تست برای هر شرکت» پیاده‌سازی شده است؛ یعنی شما برای هر شرکت، روی هر پروژه، یوزر/رمز و تاریخ انقضا می‌سازید و لینک اختصاصی تحویل می‌دهید.

ماژول مالی Textile ERP از قبل ساختار چندشرکتی دارد، اما بخش عملیاتی هنوز به بازطراحی عمیق‌تری برای جداسازی کامل داده‌های هر شرکت نیاز دارد. در این مرحله، برای آماده‌سازی سریع روی VPS، پنل مدیریت دسترسی مشتریان و لینک‌های زمان‌دار به‌عنوان لایه عملیاتی اصلی اضافه شده است.
