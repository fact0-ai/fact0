import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${(ms / 60000).toFixed(1)}m`;
}



export function formatTimestamp(ts: string): string {
  return new Date(ts).toLocaleString();
}

export function relativeTime(ts: string): string {
  const diff = Date.now() - new Date(ts).getTime();
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export const SPAN_TYPE_COLORS: Record<string, string> = {
  TOOL_CALL: "#8b5cf6",
  MODEL_INVOCATION: "#3b82f6",
  STATE_MUTATION: "#f59e0b",
  HUMAN_APPROVAL: "#10b981",
  POLICY_EVALUATION: "#ef4444",
  CUSTOM: "#6b7280",
};

export const STATUS_COLORS: Record<string, string> = {
  RUNNING: "#3b82f6",
  COMPLETED: "#10b981",
  FAILED: "#ef4444",
  CANCELLED: "#6b7280",
  STARTED: "#3b82f6",
};
