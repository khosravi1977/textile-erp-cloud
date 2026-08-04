import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "واسط امن تلگرام Textile ERP",
  description: "مسیر امن ارتباط گزارش‌های مدیریتی Textile ERP با تلگرام.",
  robots: { index: false, follow: false },
  icons: {
    icon: "/favicon.svg",
    shortcut: "/favicon.svg",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="fa" dir="rtl">
      <body>{children}</body>
    </html>
  );
}
