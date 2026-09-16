import type { MetadataRoute } from "next";
import { resolveMetadataBase } from "@/lib/app-origin";
export default function sitemap(): MetadataRoute.Sitemap {
  return [
    { url: resolveMetadataBase() },
    { url: resolveMetadataBase() + "/legal" },
  ];
}
