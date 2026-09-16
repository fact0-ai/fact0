import type { Metadata } from "next";
import localFont from "next/font/local";

import { ThemeProvider } from "@/components/theme-provider";
import { BRAND_FAVICON } from "@/lib/brand-assets";
import { resolveMetadataBase } from "@/lib/app-origin";
import { Toaster } from "sonner";
import "./globals.css";

const dmSans = localFont({
  src: [
    {
      path: "../fonts/dm-sans/DMSans-Regular.ttf",
      weight: "400",
      style: "normal",
    },
    {
      path: "../fonts/dm-sans/DMSans-Italic.ttf",
      weight: "400",
      style: "italic",
    },
    {
      path: "../fonts/dm-sans/DMSans-Medium.ttf",
      weight: "500",
      style: "normal",
    },
    {
      path: "../fonts/dm-sans/DMSans-MediumItalic.ttf",
      weight: "500",
      style: "italic",
    },
    {
      path: "../fonts/dm-sans/DMSans-SemiBold.ttf",
      weight: "600",
      style: "normal",
    },
    {
      path: "../fonts/dm-sans/DMSans-SemiBoldItalic.ttf",
      weight: "600",
      style: "italic",
    },
    {
      path: "../fonts/dm-sans/DMSans-Bold.ttf",
      weight: "700",
      style: "normal",
    },
    {
      path: "../fonts/dm-sans/DMSans-BoldItalic.ttf",
      weight: "700",
      style: "italic",
    },
  ],
  variable: "--font-dm-sans",
  display: "swap",
});

import content from "../content.json";

export const metadata: Metadata = {
  metadataBase: new URL(resolveMetadataBase()),
  title: {
    default: content.metadata.title.default,
    template: content.metadata.title.template,
  },
  description: content.metadata.description,
  manifest: BRAND_FAVICON.manifest,
  icons: {
    icon: [
      { url: BRAND_FAVICON.ico, sizes: "48x48" },
      { url: BRAND_FAVICON.svg, type: "image/svg+xml" },
      { url: BRAND_FAVICON.png16, sizes: "16x16", type: "image/png" },
      { url: BRAND_FAVICON.png32, sizes: "32x32", type: "image/png" },
    ],
    apple: [{ url: BRAND_FAVICON.apple, sizes: "180x180", type: "image/png" }],
    shortcut: [{ url: BRAND_FAVICON.ico }],
  },
  openGraph: {
    title: content.metadata.title.default,
    description: content.metadata.description,
    siteName: content.brand.siteName,
    images: [
      {
        url: content.metadata.ogImage,
        width: 1200,
        height: 630,
        alt: content.metadata.title.default,
      },
    ],
  },
  alternates: {
    canonical: "/",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body
        className={`${dmSans.variable} antialiased`}
        suppressHydrationWarning
      >
        <ThemeProvider
          attribute="class"
          defaultTheme="light"
          disableTransitionOnChange
        >
          {children}
          <Toaster position="top-right" richColors />
        </ThemeProvider>
      </body>
    </html>
  );
}
