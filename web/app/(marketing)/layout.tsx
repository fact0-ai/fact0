import localFont from "next/font/local";
import { Navbar } from "@/components/blocks/navbar";
import { Footer } from "@/components/blocks/footer";
import "./marketing.css";

const heading = localFont({ src: "../../fonts/bricolage-grotesque/BricolageGrotesque-Variable.woff2", weight: "400 800", style: "normal", variable: "--font-retro-heading", display: "swap" });
const mono = localFont({ src: "../../fonts/jetbrains-mono/JetBrainsMono-Variable.woff2", weight: "400 700", style: "normal", variable: "--font-retro-mono", display: "swap" });

export default function MarketingLayout({ children }: { children: React.ReactNode }) {
  return <div className={`retro-marketing ${heading.variable} ${mono.variable}`}><a className="retro-skip" href="#content">Skip to content</a><Navbar /><main id="content" tabIndex={-1}>{children}</main><Footer /></div>;
}
