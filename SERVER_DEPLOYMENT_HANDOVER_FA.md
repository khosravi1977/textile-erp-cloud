# مستند تحویل فنی و استقرار سامانه Textile ERP

این فایل برای تحویل پروژه به شرکت یا تیمی تهیه شده است که قرار است سامانه را روی سرور نصب، اجرا، پشتیبانی و مانیتور کند.

## 1. معرفی سامانه

سامانه Textile ERP یک نرم افزار تحت وب برای مدیریت فرآیندهای عملیاتی و مالی صنعت نساجی است. سیستم شامل دو بخش اصلی است:

- بخش عملیاتی: ثبت ورود نخ، خروج نخ، تولید، فاکتور خروج پارچه، هزینه ها و اطلاعات پایه.
- بخش مالی: مشاهده داده های عملیاتی، ثبت قیمت گذاری، مدیریت موجودی، هزینه ها، فاکتورها، گزارش ها و API مالی.

سامانه به صورت چند بخشی طراحی شده است و از طریق یک Portal مرکزی در آدرس زیر در دسترس قرار می گیرد:

```text
http://127.0.0.1:8080
```

## 2. تکنولوژی های استفاده شده

### Backend

- زبان اصلی Backend: Go 1.21
- Financial API: سرویس Go مستقل روی پورت 8081
- Operational API: سرویس Go مستقل روی پورت 8091
- Portal Server: سرویس Go برای مسیریابی و Reverse Proxy روی پورت 8080
- ارتباط با دیتابیس: PostgreSQL با درایور `github.com/lib/pq`
- پشتیبانی داده قدیمی/انتقالی: SQLite با درایور `modernc.org/sqlite`

### Frontend

- React 18
- Vite 5
- JavaScript / JSX
- CSS اختصاصی
- کتابخانه آیکن در بخش مالی: `lucide-react`
- کتابخانه barcode در بخش عملیاتی: `jsbarcode`

### Database و Infrastructure

- PostgreSQL 16 Alpine
- Redis 7 Alpine
- Prometheus 2.55.1
- Grafana 11.3.1
- Docker Compose برای اجرای سرویس های زیرساختی
- Nginx برای حالت production مالی در Docker

## 3. ساختار کلی پروژه

ریشه پروژه:

```text
F:\project\textile_erp_clean
```

ساختار اصلی:

```text
textile_erp_clean/
  financial/
    cmd/api/                 Financial API
    cmd/migrate/             اجرای migration دیتابیس
    internal/                منطق دامنه، سرویس ها، handler ها، persistence
    web/                     Frontend مالی React/Vite
    deploy/                  تنظیمات nginx/prometheus/grafana
    docker-compose.prod.yml  اجرای زیرساخت و سرویس های production

  operational_cycle_go/
    cmd/server/              Operational API و backend عملیاتی
    web/                     Frontend عملیاتی React/Vite

  portal_server/
    main.go                  Portal مرکزی و reverse proxy

  start_textile_erp.bat      فایل اصلی اجرای سامانه در ویندوز
  run_financial_api.bat      اجرای Financial API
  run_financial_frontend.bat اجرای frontend مالی
  run_operational_api.bat    اجرای Operational API
  run_portal.bat             اجرای Portal
  _bat_archive/              فایل های bat قدیمی یا آرشیوشده
```

## 4. سرویس ها و پورت ها

| سرویس | آدرس | توضیح |
|---|---:|---|
| Portal | `http://127.0.0.1:8080` | صفحه ورود مرکزی و مسیر دسترسی به مالی/عملیاتی |
| Financial Frontend | `http://127.0.0.1:5173` | رابط کاربری مالی در حالت dev/local |
| Financial API | `http://127.0.0.1:8081` | API مالی |
| Financial API Health | `http://127.0.0.1:8081/health` | کنترل سلامت API مالی |
| Metrics | `http://127.0.0.1:8081/metrics` | متریک های Prometheus |
| Operational API/UI | `http://127.0.0.1:8091` | سرویس عملیاتی |
| Grafana | `http://127.0.0.1:3000` | داشبورد مانیتورینگ |
| Prometheus | `http://127.0.0.1:9090` | جمع آوری متریک ها |
| PostgreSQL | `localhost:5433` | دیتابیس اصلی |
| PostgreSQL Replica | `localhost:5434` | replica تعریف شده برای خواندن |
| Redis | `localhost:6380` | cache |

## 5. اطلاعات ورود پیش فرض

### ورود به بخش مالی

```text
username: admin
password: admin123
```

کاربر پیش فرض برای شرکت `پرگل` ایجاد شده است.

### ورود به Grafana

```text
username: admin
password: admin123
```

## 6. نحوه اجرای برنامه روی ویندوز

پیش نیازها:

- Docker Desktop
- Go 1.21 یا بالاتر
- Node.js و npm
- دسترسی آزاد به پورت های 8080، 8081، 8091، 5173، 3000، 9090، 5433، 5434، 6380

برای اجرا:

```bat
F:\project\textile_erp_clean\start_textile_erp.bat
```

بعد از اجرا، منوی زیر باز می شود:

```text
1. Portal        - http://127.0.0.1:8080
2. Financial API - http://127.0.0.1:8081/health
3. Metrics       - http://127.0.0.1:8081/metrics
4. Grafana       - http://127.0.0.1:3000
5. Prometheus    - http://127.0.0.1:9090
0. Exit
```

برای ورود کاربر نهایی، گزینه 1 یا آدرس زیر استفاده شود:

```text
http://127.0.0.1:8080
```

## 7. مسیرهای Portal

Portal روی پورت 8080 اجرا می شود و مسیرهای زیر را مدیریت می کند:

```text
/financial/       بخش مالی
/operational/     بخش عملیاتی
/api/financial/   اتصال Portal به Financial API
/api/operational/ اتصال Portal به Operational API
/health           سلامت Portal
```

نکته مهم: در حالت local/dev، frontend مالی توسط Vite اجرا می شود و Portal مسیرهای زیر را نیز proxy می کند:

```text
/@vite/
/src/
/node_modules/
/assets/
```

## 8. دیتابیس

دیتابیس اصلی PostgreSQL است:

```text
Database: textile_erp
User: erp_user
Password: change_me
Port: 5433
```

Migration های مالی در مسیر زیر هستند:

```text
financial/internal/infrastructure/persistence/postgres/migrations
```

اجرای migration:

```bat
cd /d F:\project\textile_erp_clean\financial
go run ./cmd/migrate
```

در زمان اجرای `start_textile_erp.bat`، migration مالی به صورت خودکار اجرا می شود.

## 9. اجرای Docker Compose

فایل اصلی Docker Compose:

```text
financial/docker-compose.prod.yml
```

سرویس های تعریف شده:

- `postgres`
- `postgres-replica`
- `redis`
- `api`
- `nginx`
- `prometheus`
- `grafana`

اجرای production مالی:

```bat
cd /d F:\project\textile_erp_clean\financial
docker compose -f docker-compose.prod.yml up -d --build
```

برای اجرای فقط زیرساخت:

```bat
docker compose -f docker-compose.prod.yml up -d postgres postgres-replica redis prometheus grafana
```

## 10. متغیرهای محیطی مهم

| متغیر | مقدار پیش فرض | توضیح |
|---|---|---|
| `DB_HOST` | `localhost` | آدرس PostgreSQL |
| `DB_PORT` | `5433` | پورت PostgreSQL در host |
| `DB_USER` | `erp_user` | نام کاربری دیتابیس |
| `DB_PASSWORD` | `change_me` | رمز دیتابیس |
| `DB_NAME` | `textile_erp` | نام دیتابیس |
| `DB_SSLMODE` | `disable` | حالت SSL دیتابیس |
| `JWT_SECRET` | local secret | کلید امضای JWT |
| `FINANCIAL_ADMIN_PASSWORD` | `admin123` | رمز admin مالی |
| `APP_PORT` | `8081` | پورت Financial API |
| `PORT` | `8091` | پورت Operational API |
| `OPERATIONAL_DB_DRIVER` | `postgres` | نوع دیتابیس عملیاتی |

برای سرور واقعی باید حتما مقادیر امنیتی تغییر کنند:

- `DB_PASSWORD`
- `JWT_SECRET`
- `FINANCIAL_ADMIN_PASSWORD`
- رمز Grafana

## 11. مانیتورینگ

### Prometheus

Prometheus متریک ها را از Financial API می خواند:

```text
http://127.0.0.1:8081/metrics
```

آدرس UI:

```text
http://127.0.0.1:9090
```

### Grafana

Grafana برای نمایش داشبوردها استفاده می شود:

```text
http://127.0.0.1:3000
```

ورود پیش فرض:

```text
admin / admin123
```

## 12. نکات مهم برای استقرار روی سرور

برای محیط production پیشنهاد می شود:

1. پروژه روی یک سرور Windows Server یا Linux Server با Docker نصب شود.
2. رمزهای پیش فرض تغییر کنند.
3. دیتابیس PostgreSQL backup زمان بندی شده داشته باشد.
4. پورت های عمومی فقط از طریق reverse proxy امن منتشر شوند.
5. روی دامنه واقعی SSL فعال شود.
6. دسترسی مستقیم به PostgreSQL، Redis، Prometheus و Grafana محدود شود.
7. فایل های `.bat` برای Windows مناسب هستند؛ برای Linux باید معادل shell script ساخته شود.
8. در production بهتر است frontend ها build شوند و به جای Vite dev server از nginx/static hosting استفاده شود.

## 13. وضعیت تست فعلی

در محیط فعلی موارد زیر تست شده اند:

- اجرای PostgreSQL، Redis، Prometheus و Grafana
- اجرای Financial API
- اجرای Operational API
- اجرای Portal
- اجرای frontend مالی از مسیر Portal
- ورود مالی با `admin / admin123`
- ایجاد و مشاهده داده های شرکت `پرگل`
- ثبت ورود نخ، خروج نخ، فاکتور خروج پارچه و هزینه در بخش عملیاتی
- مشاهده همان داده ها از API مالی
- ثبت قیمت گذاری و رکوردهای مالی مرتبط
- اجرای تست های Go در پروژه مالی با دستور:

```bat
cd /d F:\project\textile_erp_clean\financial
go test ./...
```

## 14. چک لیست تحویل به شرکت اجراکننده

شرکت اجراکننده قبل از تحویل نهایی روی سرور باید این موارد را انجام دهد:

- نصب Docker و Docker Compose
- نصب Go 1.21+
- نصب Node.js و npm
- تنظیم رمزهای production
- اجرای migration دیتابیس
- اجرای سرویس ها
- تست health endpoint ها
- تست ورود admin
- تست ثبت یک رکورد عملیاتی
- تست مشاهده رکورد عملیاتی در بخش مالی
- فعال سازی backup دیتابیس
- فعال سازی SSL در صورت اتصال به اینترنت یا شبکه سازمانی

## 15. آدرس های کنترل سلامت

```text
Portal:
http://127.0.0.1:8080/health

Financial API:
http://127.0.0.1:8081/health

Metrics:
http://127.0.0.1:8081/metrics

Grafana:
http://127.0.0.1:3000/api/health

Prometheus:
http://127.0.0.1:9090/-/ready
```

## 16. جمع بندی

این پروژه یک سامانه ERP تحت وب برای صنعت نساجی است که با معماری چند سرویسه سبک پیاده سازی شده است. هسته backend با Go توسعه داده شده، رابط کاربری با React/Vite ساخته شده و داده ها در PostgreSQL نگهداری می شوند. برای مانیتورینگ از Prometheus و Grafana استفاده شده و اجرای زیرساخت با Docker Compose انجام می شود.

در حالت فعلی سیستم برای اجرای local/server داخلی آماده است. برای production عمومی، تغییر رمزها، تنظیم SSL، محدودسازی دسترسی پورت ها، backup دیتابیس و اجرای frontend به صورت build/static توصیه می شود.
