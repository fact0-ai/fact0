"use client";

import { useEffect, useState } from "react";
import { Check, Copy } from "lucide-react";

export function CodePanel({ code, filename, caption }: { code: string; filename: string; caption?: string }) {
  const [status, setStatus] = useState<"idle" | "copied" | "error">("idle");
  useEffect(() => {
    if (status === "idle") return;
    const timer = setTimeout(() => setStatus("idle"), 2400);
    return () => clearTimeout(timer);
  }, [status]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(code);
      setStatus("copied");
    } catch {
      setStatus("error");
    }
  }

  return (
    <div className="retro-command-wrap">
      <div className="retro-command">
        <div className="retro-command-header">
          <span>{filename}</span>
          <button type="button" onClick={copy} aria-label={`Copy ${filename}`}>
            {status === "copied" ? <Check size={14} /> : <Copy size={14} />}
            <span aria-live="polite">{status === "copied" ? "COPIED" : status === "error" ? "SELECT CODE TO COPY" : "COPY"}</span>
          </button>
        </div>
        <pre tabIndex={0}><code>{code}</code></pre>
      </div>
      {caption && <p className="retro-caption">{caption}</p>}
    </div>
  );
}
