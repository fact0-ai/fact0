import type { telemetryClient } from "@/lib/api";
import {
  findDemoSpan,
  getDemoDAG,
  getDemoExecution,
  getDemoReplay,
  getDemoSpans,
  listDemoExecutions,
} from "@/lib/demo/fixtures";

export type DemoTelemetryClient = ReturnType<typeof telemetryClient>;

export function createDemoTelemetryClient(): DemoTelemetryClient {
  return {
    listExecutions: (params) => Promise.resolve(listDemoExecutions(params)),
    getExecution: (id) => {
      const exec = getDemoExecution(id);
      if (!exec) return Promise.reject(new Error("execution not found"));
      return Promise.resolve(exec);
    },
    getSpans: (executionId) =>
      Promise.resolve({ spans: getDemoSpans(executionId) }),
    getSpan: (spanId) => {
      const span = findDemoSpan(spanId);
      if (!span) return Promise.reject(new Error("span not found"));
      return Promise.resolve({ span, events: [] });
    },
    getSpanEvents: () => Promise.resolve({ events: [] }),
    getExecutionDAG: (executionId) => Promise.resolve(getDemoDAG(executionId)),
    replayExecution: (executionId) => Promise.resolve(getDemoReplay(executionId)),
  };
}
