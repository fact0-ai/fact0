import Link from "next/link";
import { ArrowUpRight } from "lucide-react";
import { docsHref } from "@/lib/docs-origin";

export function Footer() {
  return <footer className="retro-footer">
    <div className="retro-container">
      <div className="retro-footer-cta"><div><p className="retro-eyebrow">[ YOUR NEXT RUN, WITH A RECORD ]</p><h2>Keep the history.<br /><span className="retro-highlight retro-highlight-yellow">Own the instance.</span></h2></div><a className="retro-button retro-button-coral" href="https://github.com/fact0-ai/fact0#quickstart">Run Fact0 locally <ArrowUpRight size={20} /></a></div>
      <nav className="retro-footer-links" aria-label="Footer navigation"><a href={docsHref()}>Documentation</a><a href="https://github.com/fact0-ai/fact0">GitHub <ArrowUpRight size={13} /></a><a href="https://github.com/fact0-ai/fact0/issues">Issues <ArrowUpRight size={13} /></a><Link href="/legal">License & privacy</Link></nav>
      <div className="retro-footer-meta"><span>© {new Date().getFullYear()} FACT0 / MIT LICENSED</span><span>EXPERIMENTAL. BEST-EFFORT MAINTENANCE.</span></div>
      <div className="retro-footer-wordmark" aria-hidden="true">fact0</div>
    </div>
  </footer>;
}
