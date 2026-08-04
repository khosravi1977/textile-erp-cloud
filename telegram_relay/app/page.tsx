export default function Home() {
  return (
    <main>
      <section className="status-card" aria-labelledby="relay-title">
        <div className="status-dot" aria-hidden="true" />
        <p className="eyebrow">Textile ERP</p>
        <h1 id="relay-title">واسط امن گزارش‌های تلگرام</h1>
        <p>
          این سرویس فقط برای ارتباط رمزگذاری‌شدهٔ سامانهٔ نساجی با تلگرام
          استفاده می‌شود و رابط عمومی کاربری ندارد.
        </p>
        <div className="status-line">
          <span>وضعیت سرویس</span>
          <strong>فعال</strong>
        </div>
      </section>
    </main>
  );
}
