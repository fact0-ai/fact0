import type { AuditFilter } from "@/lib/audit-types";
import type { AuditClient } from "@/lib/audit-api";
import { demoClaudeCodeEvents } from "@/lib/demo/demo-claude-code";
import {
  DEMO_VERIFY_RESULT,
  demoEvidencePackBlob,
  demoPdfBlob,
  filterDemoAuditEvents,
  getDemoAuditEvent,
} from "@/lib/demo/fixtures";

export function createDemoAuditClient(): AuditClient {
  return {
    listEvents: (filter: AuditFilter) =>
      Promise.resolve(
        // Coding-agent queries route to the dedicated session fixtures so the
        // Coding Agents timeline works in demo mode.
        filter.action?.startsWith("claude_code")
          ? demoClaudeCodeEvents(filter)
          : filterDemoAuditEvents(filter),
      ),
    getEvent: (id: string) => {
      const evt = getDemoAuditEvent(id);
      if (!evt) return Promise.reject(new Error("not found"));
      return Promise.resolve(evt);
    },
    verifyChain: () => Promise.resolve(DEMO_VERIFY_RESULT),
    verifyDeep: () => Promise.resolve(DEMO_VERIFY_RESULT),
    downloadPDF: () => Promise.resolve(demoPdfBlob()),
    downloadEvidencePack: () => Promise.resolve(demoEvidencePackBlob()),
    reanchorChain: () => Promise.reject(new Error("Sign up to use chain repair")),
    reanchorAll: () => Promise.reject(new Error("Sign up to use chain repair")),
    sseTicket: () => Promise.resolve({ ticket: "demo", expires_in: 60 }),
  };
}
