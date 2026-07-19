# راهنمای بسته ZIP تحویلی

این پوشه برای ارسال به شرکت اجراکننده پاکسازی شده است.

## موارد حذف شده برای سبک شدن ZIP

- `node_modules`
- خروجی های build مثل `dist`
- فایل های اجرایی موقت مثل `*.exe`
- فایل های backup و آرشیوهای قدیمی
- نسخه قدیمی Python عملیاتی
- مستندات تکراری و فایل های موقت

## موارد نگهداری شده

- سورس کامل Financial API
- سورس کامل Operational API
- سورس کامل Portal
- سورس frontend مالی و عملیاتی
- migration های دیتابیس
- Docker Compose و تنظیمات مانیتورینگ
- فایل اصلی اجرا: `start_textile_erp.bat`
- مستند تحویل فنی: `SERVER_DEPLOYMENT_HANDOVER_FA.md`

## بعد از Extract روی سرور

روی سرور باید وابستگی های frontend دوباره نصب شوند:

```bat
cd /d financial\web
npm install

cd /d ..\..\operational_cycle_go\web
npm install
```

سپس اجرای سیستم:

```bat
cd /d F:\project\textile_erp_clean
start_textile_erp.bat
```

اگر مسیر پروژه روی سرور متفاوت است، ابتدا وارد همان پوشه شوید و سپس `start_textile_erp.bat` را اجرا کنید.
