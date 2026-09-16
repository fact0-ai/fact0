import Link from "next/link";
export function Footer() {
  return (
    <footer className="mx-auto max-w-6xl px-6 py-10 border-t flex flex-wrap justify-between gap-4 text-xs text-muted-foreground">
      <span>Fact0 · Experimental self-hosted software</span>
      <div className="flex gap-5">
        <Link href="/legal">License & privacy</Link>
        <a href="https://github.com/fact0-ai/fact0/issues">Issues</a>
      </div>
    </footer>
  );
}
