import Link from "next/link";
import { BrandLogo } from "@/components/brand/brand-logo";
import { docsHref } from "@/lib/docs-origin";
export function Navbar() {
  const marketingOnly = process.env.NEXT_PUBLIC_MARKETING_ONLY === "1";
  return (
    <header className="mx-auto max-w-6xl flex items-center justify-between gap-6 px-6 py-6 border-b border-zinc-200/60">
      <BrandLogo />
      <nav className="flex items-center gap-5 text-sm">
        <a href={docsHref()}>Docs</a>
        <a href="https://github.com/fact0-ai/fact0">GitHub</a>
        <Link
          className="rounded-lg border px-4 py-2"
          href={
            marketingOnly
              ? "https://github.com/fact0-ai/fact0#quickstart"
              : "/dashboard"
          }
        >
          {marketingOnly ? "Run locally" : "Open dashboard"}
        </Link>
      </nav>
    </header>
  );
}
