import type { Metadata, Viewport } from "next";
import "./globals.css";
import { Providers } from "./providers";
import { RegisterServiceWorker } from "@/components/pwa/register-sw";

export const metadata: Metadata = {
  title: "VoDog",
  description: "短信中枢：国内线 / 国外线",
  applicationName: "VoDog",
  manifest: "/manifest.webmanifest",
  appleWebApp: {
    capable: true,
    title: "VoDog",
    statusBarStyle: "default",
  },
  icons: {
    icon: [
      { url: "/icons/icon.svg", type: "image/svg+xml" },
      { url: "/icons/icon-192.png", sizes: "192x192", type: "image/png" },
    ],
    apple: [{ url: "/icons/icon-192.png", sizes: "192x192" }],
  },
};

export const viewport: Viewport = {
  themeColor: "#0a0a0a",
  width: "device-width",
  initialScale: 1,
  viewportFit: "cover",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    // suppressHydrationWarning: next-themes 在首帧前写入 class，与 SSR 输出必然不一致
    <html lang="zh-CN" suppressHydrationWarning className="h-full antialiased">
      <body className="min-h-full flex flex-col bg-background text-foreground">
        <Providers>
          <RegisterServiceWorker />
          {children}
        </Providers>
      </body>
    </html>
  );
}
