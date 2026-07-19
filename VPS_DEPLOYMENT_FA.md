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
