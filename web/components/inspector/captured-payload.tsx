"use client";
import { stringifyCapturedJSON } from "@/lib/captured-json";
import { useState } from "react";
/** Preserve the captured value; expand, copy or download without a preview truncation. */
export function CapturedPayload({
  label,
  value,
  status,
  reason,
}: {
  label: string;
  value: unknown;
  status?: string;
  reason?: string;
}) {
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  const text = typeof value === "string" ? value : stringifyCapturedJSON(value);
  const available = value !== undefined && value !== null;
  return (
    <details className="mt-2 rounded-lg border bg-muted/20">
      <summary className="cursor-pointer px-3 py-2 text-xs font-medium">
        {label}
        {status && (
          <span className="ml-2 font-normal text-muted-foreground">
            {status}
          </span>
        )}
      </summary>
      <div className="px-3 pb-3 space-y-2">
        {reason && <p className="text-xs text-muted-foreground">{reason}</p>}
        {available ? (
          <>
            <div className="flex gap-3 text-xs">
              <button
                className="underline"
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(text);
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                  } catch {
                    setError("Could not copy. Download the payload instead.");
                  }
                }}
              >
                {copied ? "Copied" : "Copy"}
              </button>
              <button
                className="underline"
                onClick={() => {
                  const url = URL.createObjectURL(
                    new Blob([text], { type: "text/plain;charset=utf-8" }),
                  );
                  const a = document.createElement("a");
                  a.href = url;
                  a.download =
                    label.toLowerCase().replace(/[^a-z0-9]+/g, "-") + ".txt";
                  a.click();
                  setTimeout(() => URL.revokeObjectURL(url), 1000);
                }}
              >
                Download
              </button>
            </div>
            <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-words text-xs font-mono">
              {text}
            </pre>
          </>
        ) : (
          <p className="text-xs text-muted-foreground">
            Not captured. No content is inferred.
          </p>
        )}
        {error && (
          <p role="alert" className="text-xs text-red-600">
            {error}
          </p>
        )}
      </div>
    </details>
  );
}
