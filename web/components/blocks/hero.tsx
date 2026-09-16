import { ArrowDown, ArrowUpRight, Braces, FileCheck2, GitBranch } from "lucide-react";
import { docsHref } from "@/lib/docs-origin";
import { AgentFlowViz } from "./agent-flow-viz";
import { heroFeatures } from "./marketing-content";

const icons = [GitBranch, Braces, FileCheck2];

export function Hero() {
  return (
    <section className="retro-hero">
      <div className="retro-container">
        <div className="retro-hero-grid">
          <div>
            <p className="retro-eyebrow">[ A RECORD OF YOUR AGENT&apos;S WORK ]</p>
            <h1>See what your agents <span className="retro-highlight">actually did.</span></h1>
            <p className="retro-lead">Capture Python runs and Claude Code sessions. Inspect prompts, tool calls and results, replay recorded traces, and verify audit history in one local workspace.</p>
            <div className="retro-actions">
              <a className="retro-button" href="https://github.com/fact0-ai/fact0#quickstart">Run locally <ArrowUpRight size={18} /></a>
              <a className="retro-button retro-button-paper" href={docsHref()}>Read the docs <ArrowUpRight size={18} /></a>
            </div>
            <p className="retro-smallprint">MIT licensed. One owner. Your infrastructure.</p>
          </div>
          <aside className="retro-hero-index" aria-label="What Fact0 records">
            <div className="retro-index-title"><span>THE SHORT VERSION</span><span>01—03</span></div>
            {heroFeatures.map((feature, i) => {
              const Icon = icons[i];
              return <div className="retro-index-item" key={feature.title}>
                <Icon size={22} strokeWidth={1.7} />
                <div><h2>{feature.title}</h2><p>{feature.description}</p></div>
              </div>;
            })}
            <a className="retro-index-link" href="#how-it-works">HOW IT FITS TOGETHER <ArrowDown size={16} /></a>
          </aside>
        </div>
        <AgentFlowViz />
        <p className="retro-caption retro-center">Synthetic examples. No agent is running on this page. Your dashboard shows your captured data.</p>
      </div>
    </section>
  );
}
