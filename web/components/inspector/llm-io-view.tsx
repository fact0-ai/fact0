"use client";

import { useState } from "react";
import type { PayloadRef } from "@/lib/types";
import { Check, Copy } from "lucide-react";

// Renders LLM prompt/completion payloads the way developers expect from an
// observability tool: chat-formatted messages by default, raw JSON on toggle.
// Understands the SDK wire shapes: {"messages": [{role, content}, ...]}, a
// single {role, content} object, a bare message array, or plain text.

type ChatMessage = {
  role: string;
  content: unknown;
  tool_calls?: unknown[];
  name?: string;
};

export type IOViewMode = "pretty" | "json";

function isMessage(value: unknown): value is ChatMessage {
  return (
    !!value &&
    typeof value === "object" &&
    typeof (value as ChatMessage).role === "string" &&
    "content" in (value as object)
  );
}

function toMessages(inline: unknown): ChatMessage[] | null {
  let data = inline;
  if (typeof data === "string") {
    const trimmed = data.trim();
    if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) return null;
    try {
      data = JSON.parse(trimmed);
    } catch {
      return null;
    }
  }
  if (Array.isArray(data)) {
    return data.every(isMessage) ? (data as ChatMessage[]) : null;
  }
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    if (Array.isArray(obj.messages) && obj.messages.every(isMessage)) {
      return obj.messages as ChatMessage[];
    }
    if (isMessage(obj)) return [obj];
  }
  return null;
}

function contentToText(content: unknown): string {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        if (typeof part === "string") return part;
        if (part && typeof part === "object") {
          const p = part as Record<string, unknown>;
          if (typeof p.text === "string") return p.text;
          return JSON.stringify(p);
        }
        return String(part);
      })
      .join("\n");
  }
  return JSON.stringify(content, null, 2);
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

const ROLE_STYLES: Record<string, { label: string; bubble: string; badge: string }> = {
  system: {
    label: "System",
    bubble: "bg-muted/60 border-border",
    badge: "text-muted-foreground bg-muted",
  },
  user: {
    label: "User",
    bubble: "bg-indigo-500/5 border-indigo-500/20",
    badge: "text-indigo-600 dark:text-indigo-400 bg-indigo-500/10",
  },
  assistant: {
    label: "Assistant",
    bubble: "bg-emerald-500/5 border-emerald-500/20",
    badge: "text-emerald-600 dark:text-emerald-400 bg-emerald-500/10",
  },
  tool: {
    label: "Tool",
    bubble: "bg-amber-500/5 border-amber-500/20",
    badge: "text-amber-600 dark:text-amber-400 bg-amber-500/10",
  },
  function: {
    label: "Function",
    bubble: "bg-amber-500/5 border-amber-500/20",
    badge: "text-amber-600 dark:text-amber-400 bg-amber-500/10",
  },
};

function MessageBubble({ message }: { message: ChatMessage }) {
  const style = ROLE_STYLES[message.role] ?? {
    label: message.role,
    bubble: "bg-muted/60 border-border",
    badge: "text-muted-foreground bg-muted",
  };
  const text = contentToText(message.content);
  return (
    <div className={`rounded-lg border px-2.5 py-2 ${style.bubble}`}>
      <div className="flex items-center gap-1.5 mb-1">
        <span
          className={`inline-flex px-1.5 py-0.5 rounded text-[9px] font-black uppercase tracking-wider ${style.badge}`}
        >
          {style.label}
        </span>
        {message.name && (
          <span className="text-[9px] text-muted-foreground font-mono">{message.name}</span>
        )}
      </div>
      {text && (
        <div className="text-xs text-foreground/90 whitespace-pre-wrap break-words leading-relaxed">
          {text}
        </div>
      )}
      {Array.isArray(message.tool_calls) && message.tool_calls.length > 0 && (
        <pre className="mt-1.5 text-[10px] text-foreground/70 bg-muted rounded p-1.5 overflow-x-auto font-mono">
          {JSON.stringify(message.tool_calls, null, 2)}
        </pre>
      )}
    </div>
  );
}

function CopyButton({ payload }: { payload: PayloadRef }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      onClick={() => {
        const text =
          typeof payload.inline === "string"
            ? payload.inline
            : JSON.stringify(payload.inline, null, 2);
        navigator.clipboard.writeText(text).then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        });
      }}
      className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
      title="Copy raw payload"
    >
      {copied ? <Check className="size-3 text-emerald-500" /> : <Copy className="size-3" />}
    </button>
  );
}

export function LLMPayloadBlock({
  label,
  payload,
  mode,
}: {
  label: string;
  payload: PayloadRef;
  mode: IOViewMode;
}) {
  if (payload.inline == null) {
    return (
      <div className="mt-1">
        <span className="text-[9px] text-muted-foreground uppercase tracking-wider">{label}</span>
        <div className="mt-0.5 text-[10px] text-muted-foreground bg-muted/50 border border-dashed border-border rounded-lg p-2">
          {payload.blob_ref
            ? `Payload stored externally (${formatBytes(payload.size_bytes)}) — inline preview unavailable.`
            : "No payload captured."}
        </div>
      </div>
    );
  }

  const messages = mode === "pretty" ? toMessages(payload.inline) : null;

  return (
    <div className="mt-1">
      <div className="flex items-center justify-between">
        <span className="text-[9px] text-muted-foreground uppercase tracking-wider">
          {label}
          <span className="ml-1.5 normal-case tracking-normal text-muted-foreground/60">
            {formatBytes(payload.size_bytes)}
          </span>
        </span>
        <CopyButton payload={payload} />
      </div>
      <div className="mt-1 max-h-72 overflow-y-auto space-y-1.5 pr-0.5">
        {mode === "pretty" && messages ? (
          messages.map((m, i) => <MessageBubble key={i} message={m} />)
        ) : mode === "pretty" && typeof payload.inline === "string" ? (
          <div className="text-xs text-foreground/90 whitespace-pre-wrap break-words bg-muted/50 border border-border rounded-lg p-2 leading-relaxed">
            {payload.inline}
          </div>
        ) : (
          <pre className="text-[10px] text-foreground/80 bg-muted rounded-lg p-2 overflow-x-auto font-mono border border-border">
            {typeof payload.inline === "string"
              ? payload.inline
              : JSON.stringify(payload.inline, null, 2)}
          </pre>
        )}
      </div>
    </div>
  );
}

export function IOModeToggle({
  mode,
  onChange,
}: {
  mode: IOViewMode;
  onChange: (mode: IOViewMode) => void;
}) {
  return (
    <div className="inline-flex rounded-lg border border-border overflow-hidden">
      {(["pretty", "json"] as const).map((m) => (
        <button
          key={m}
          onClick={() => onChange(m)}
          className={`px-2 py-0.5 text-[9px] font-black uppercase tracking-wider transition-colors ${
            mode === m
              ? "bg-foreground text-background"
              : "bg-transparent text-muted-foreground hover:text-foreground"
          }`}
        >
          {m === "pretty" ? "Pretty" : "JSON"}
        </button>
      ))}
    </div>
  );
}
