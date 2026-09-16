"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import { ArrowDown, ArrowRight, ArrowUpRight, Braces, Check, Circle, Code2, Database, FileText, Pause, Play, TerminalSquare } from "lucide-react";
import { docsHref } from "@/lib/docs-origin";

const examples = [
  { id: "python", label: "Python agent", inputs: ["your agent", "Fact0 SDK", "tool results", "local API"], steps: ["Calculate a total", "add(19, 23)", "Handled failure"], result: "42", kind: "CUSTOM / TOOL", events: 8 },
  { id: "claude", label: "Claude Code", inputs: ["Claude Code", "local hooks", "transcript", "local API"], steps: ["Prompt submitted", "Read src/agent.py", "Run pytest", "Assistant response"], result: "inspect", kind: "PROMPT / TOOL / TEXT", events: 12 },
] as const;
const inputIcons = [Code2, Braces, FileText, Database];

function subscribeMotion(callback: () => void) {
  const media = window.matchMedia("(prefers-reduced-motion: reduce)");
  media.addEventListener("change", callback);
  return () => media.removeEventListener("change", callback);
}

export function AgentFlowViz() {
  const [selected, setSelected] = useState(0);
  const [position, setPosition] = useState(0);
  const [paused, setPaused] = useState(false);
  const reducedMotion = useSyncExternalStore(subscribeMotion,
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches, () => true);
  const example = examples[selected];
  const animate = !paused && !reducedMotion;
  const visible = reducedMotion ? example.steps.length : Math.min(position, example.steps.length);
  useEffect(() => {
    if (!animate) return;
    const timer = setInterval(() => setPosition(value => (value + 1) % (example.steps.length + 3)), 1050);
    return () => clearInterval(timer);
  }, [animate, example.steps.length]);

  return <div className="retro-debugger" aria-label="Illustrated Fact0 capture and inspection workflow">
    <div className="retro-debugger-title"><span><TerminalSquare size={17} /> FACT0 / EXECUTION.REC</span><span className="retro-debugger-stamp">SYNTHETIC EXAMPLE</span></div>
    <div className="retro-debugger-toolbar">
      <div className="retro-debugger-tabs">{examples.map((item, i) => <button type="button" aria-pressed={selected === i} key={item.id} onClick={() => { setSelected(i); setPosition(0); }}>{item.label}</button>)}</div>
      <button className="retro-debugger-play" type="button" disabled={reducedMotion} onClick={() => setPaused(value => !value)} aria-label={reducedMotion ? "Static illustration: reduced motion enabled" : paused ? "Play illustration" : "Pause illustration"}>{paused || reducedMotion ? <Play size={13} /> : <Pause size={13} />}<span>{reducedMotion ? "STATIC VIEW" : paused ? "PLAY" : "PAUSE"}</span></button>
    </div>
    <div className="retro-debugger-grid">
      <div className="retro-debugger-cell retro-debugger-inputs"><p className="retro-debugger-label">01 / INPUTS</p><div className="retro-source-list">{example.inputs.map((input, index) => {
        const Icon = inputIcons[index];
        return <div key={input} data-active={visible > 0 && index === (visible - 1) % example.inputs.length}><Icon size={18} /><span>{input}</span></div>;
      })}</div><p className="retro-debugger-note">Your code.<br />Your chosen capture.</p></div>
      <div className="retro-debugger-cell retro-debugger-capture"><p className="retro-debugger-label">02 / CAPTURE</p><div className="retro-capture-symbol" data-active={visible > 0}><span>f0</span></div><div className="retro-capture-arrow"><ArrowRight size={30} /></div><p className="retro-debugger-note">SDK events +<br />supported hooks</p></div>
      <div className="retro-debugger-cell retro-debugger-steps"><p className="retro-debugger-label">03 / EXECUTION</p><p className="retro-debugger-kind">{example.kind}</p><div className="retro-span-list">{example.steps.map((step, i) => <div className="retro-span-step" key={step}><div data-state={i < visible ? "recorded" : "waiting"}><span>{i < visible ? <Check size={13} /> : <Circle size={10} />}</span><code>{step}</code><small>{String(i + 1).padStart(2, "0")}</small></div>{i < example.steps.length - 1 && <ArrowDown className="retro-span-arrow" size={17} />}</div>)}</div></div>
      <div className="retro-debugger-cell retro-debugger-record"><p className="retro-debugger-label">04 / RECORD</p><div className="retro-record-count"><strong>{visible === example.steps.length ? example.events : Math.round(example.events * visible / example.steps.length)}</strong><span>EXAMPLE EVENTS</span></div><div className="retro-record-result"><span>STORED VALUE</span><strong>{example.result}</strong></div><a href={docsHref("guides/verification")}>PDF + ZIP GUIDE <ArrowUpRight size={14} /></a></div>
    </div>
    <div className="retro-debugger-footer"><span>LOCAL INSTANCE → CAPTURE → INSPECT</span><span>REPLAY THE RECORD. NOT THE AGENT.</span></div>
  </div>;
}
