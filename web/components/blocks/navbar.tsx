"use client";

import { useRef, useState } from "react";
import Link from "next/link";
import { ArrowUpRight, Menu, X } from "lucide-react";
import { BrandLogo } from "@/components/brand/brand-logo";
import { docsHref } from "@/lib/docs-origin";

const links = [
  { label: "How it works", href: "/#how-it-works" },
  { label: "Integrations", href: "/#integrations" },
  { label: "FAQ", href: "/#faq" },
];

export function Navbar() {
  const [open, setOpen] = useState(false);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const marketingOnly = process.env.NEXT_PUBLIC_MARKETING_ONLY === "1";
  return <>
    <div className="retro-banner"><span>OPEN SOURCE / MIT LICENSED</span><span className="retro-banner-extra">ONE OWNER. YOUR INFRASTRUCTURE.</span><a href="https://github.com/fact0-ai/fact0">VIEW SOURCE <ArrowUpRight size={12} /></a></div>
    <header className="retro-header" onKeyDown={event => { if (open && event.key === "Escape") { setOpen(false); toggleRef.current?.focus(); } }}>
      <div className="retro-container retro-header-row">
        <div className="retro-brand"><BrandLogo /></div>
        <nav className="retro-nav-desktop" aria-label="Main navigation">{links.map(link => <a key={link.href} href={link.href}>{link.label}</a>)}<a href={docsHref()}>Docs</a><a href="https://github.com/fact0-ai/fact0">GitHub</a></nav>
        <Link className="retro-button retro-nav-cta" href={marketingOnly ? "https://github.com/fact0-ai/fact0#quickstart" : "/dashboard"}>{marketingOnly ? "Run locally" : "Open dashboard"}<ArrowUpRight size={15} /></Link>
        <button ref={toggleRef} className="retro-menu-toggle" type="button" onClick={() => setOpen(value => !value)} aria-expanded={open} aria-controls="retro-mobile-nav" aria-label={open ? "Close navigation" : "Open navigation"}>{open ? <X size={20} /> : <Menu size={20} />}</button>
      </div>
      {open && <nav id="retro-mobile-nav" className="retro-nav-mobile" aria-label="Mobile navigation">{[...links, { label: "Docs", href: docsHref() }, { label: "GitHub", href: "https://github.com/fact0-ai/fact0" }].map(link => <a key={link.label} href={link.href} onClick={() => setOpen(false)}>{link.label}<ArrowUpRight size={15} /></a>)}</nav>}
    </header>
  </>;
}
