"use client";

import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect } from "react";

const COMPLETED_KEY = "fact0_onboarding_tour_completed";
const PENDING_STEP_KEY = "fact0_tour_step";

export interface TourStepDef {
  index: number;
  selector: string;
  /** Route prefix this step lives on (e.g. "/dashboard/settings/api-keys") */
  page: string;
  /** If true, silently skip when element not in DOM */
  optional?: boolean;
  title: string;
  description: string;
}

const TOUR_STEPS: TourStepDef[] = [
  {
    index: 0,
    selector: "[data-tour=\"api-keys-section\"]",
    page: "/dashboard/settings/api-keys",
    title: "Ingestion Credentials",
    description:
      "Provision write-scoped API keys to authenticate your agent's tracing client. The API stores a SHA-256 hash of the secret.",
  },
  {
    index: 1,
    selector: "[data-tour=\"sdk-setup-section\"]",
    page: "/dashboard",
    optional: true,
    title: "Instrument Your Runtime",
    description:
      "Integrate our Python SDK or Claude Code collector or execute direct HTTP telemetry requests. Initialize the client to begin capturing agent execution spans and tracing causality.",
  },
  {
    index: 2,
    selector: "[data-tour=\"live-feed\"]",
    page: "/dashboard",
    title: "Real-time Telemetry Stream",
    description:
      "Monitor incoming telemetry frames as they are appended. Server-Sent Events (SSE) push execution states, span lifetimes, and system errors directly to this console.",
  },
  {
    index: 3,
    selector: "[data-tour=\"execution-graph\"]",
    page: "/dashboard/executions/",
    optional: true,
    title: "Trace Causality Graph",
    description:
      "Inspect execution flow via an interactive Directed Acyclic Graph (DAG). Review parent-child span boundaries, trace variable scopes, and isolate execution faults.",
  },
  {
    index: 4,
    selector: "[data-tour=\"waterfall-chart\"]",
    page: "/dashboard/executions/",
    optional: true,
    title: "Timing & Latency Profile",
    description:
      "Examine concurrent tool runs and model invocations. Fact0 highlights the critical path automatically to identify execution bottlenecks in your agent pipeline.",
  },
  // ─── Per-page local steps (index 100+) ───────────────────────────────────
  // These are shown only on their own page and never cross-navigate.
  {
    index: 100,
    selector: "[data-tour=\"observability-tabs\"]",
    page: "/dashboard/observability",
    title: "Observability Tabs",
    description:
      "Switch between LLM Calls, Tool Executions, Error Insights, and Conversations to explore different angles of your agent's runtime behaviour.",
  },
  {
    index: 101,
    selector: "[data-tour=\"observability-metrics\"]",
    page: "/dashboard/observability",
    optional: true,
    title: "Live Metrics",
    description:
      "Aggregated call counts, token usage, latency percentiles, and cost estimates - refreshed on every page load.",
  },
  {
    index: 110,
    selector: "[data-tour=\"sessions-list\"]",
    page: "/dashboard/observability/sessions",
    title: "Conversation Sessions",
    description:
      "Each card is a multi-turn session. Fact0 groups executions by the session_id you supply in your SDK span metadata.",
  },
  {
    index: 111,
    selector: "[data-tour=\"session-detail\"]",
    page: "/dashboard/observability/sessions",
    optional: true,
    title: "Session Trace",
    description:
      "Drill into individual turns, nested LLM calls, tool invocations, token counts, and per-session cost breakdown.",
  },
  {
    index: 120,
    selector: "[data-tour=\"prompts-list\"]",
    page: "/dashboard/observability/prompts",
    title: "Prompt Catalog",
    description:
      "Versioned prompt templates your agents fetch at runtime. Push new versions via the SDK or API without redeploying.",
  },
  {
    index: 121,
    selector: "[data-tour=\"prompt-detail\"]",
    page: "/dashboard/observability/prompts",
    optional: true,
    title: "Template & Usage",
    description:
      "Preview the active template, inspect variables, and see per-version token and latency averages derived from live spans.",
  },
  {
    index: 130,
    selector: "[data-tour=\"audit-events-table\"]",
    page: "/dashboard/audit",
    title: "Audit Events",
    description:
      "Captured actions are appended as hash-chained events. Filter by agent, type, or time to narrow investigations.",
  },
  {
    index: 131,
    selector: "[data-tour=\"audit-verify-chain\"]",
    page: "/dashboard/audit",
    optional: true,
    title: "Chain Verification",
    description:
      "Recompute the stored event chain to detect modifications. This cannot prove that every action was captured.",
  },
  {
    index: 140,
    selector: "[data-tour=\"executions-list\"]",
    page: "/dashboard/executions",
    title: "Execution Traces",
    description:
      "Each row is a complete agent run. Click any execution to inspect its span DAG, waterfall timing, and causality graph.",
  },
  {
    index: 150,
    selector: "[data-tour=\"replay-list\"]",
    page: "/dashboard/replay",
    title: "Replay Browser",
    description:
      "Browse past execution traces filtered by status. Click 'Open Replay' on any completed run to step through its spans deterministically.",
  },
];

/** Poll until selector appears in DOM (up to `timeout` ms), then resolve. */
function waitForElement(
  selector: string,
  timeout = 5000,
): Promise<Element | null> {
  return new Promise((resolve) => {
    const el = document.querySelector(selector);
    if (el) return resolve(el);

    const deadline = performance.now() + timeout;
    const tick = () => {
      const found = document.querySelector(selector);
      if (found) return resolve(found);
      if (performance.now() >= deadline) return resolve(null);
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
}

/** Whether the given pathname matches a step's page definition. */
function matchesPage(pathname: string, stepPage: string): boolean {
  // Pages ending with / use prefix matching (e.g. "/dashboard/executions/")
  if (stepPage.endsWith("/")) return pathname.startsWith(stepPage);
  // All others require an exact match so "/dashboard" doesn't match
  // "/dashboard/settings/api-keys" and vice-versa.
  return pathname === stepPage;
}

/** Steps that belong to the given pathname. */
function stepsForPath(pathname: string): TourStepDef[] {
  return TOUR_STEPS.filter((s) => matchesPage(pathname, s.page));
}

export function useTour() {
  const router = useRouter();
  const pathname = usePathname();

  // Preload driver.js on mount so the first click is instant.
  useEffect(() => {
    void Promise.all([
      import("driver.js"),
      import("driver.js/dist/driver.css"),
    ]).catch(() => { });
  }, []);

  const startTour = useCallback(
    async (fromStep = 0, allowCrossPage = true) => {
      // Mobile guard - skip on small viewports
      if (typeof window !== "undefined" && window.matchMedia("(max-width: 768px)").matches) {
        return;
      }

      // Dynamically import driver.js to avoid SSR issues
      const { driver } = await import("driver.js");
      await import("driver.js/dist/driver.css");

      const currentPath = pathname;

      // If the first step to show is on a different page, navigate there first.
      // TourAutoResume will resume the tour once that page mounts.
      const firstStepDef = TOUR_STEPS.find((s) => s.index === fromStep);
      if (firstStepDef && !matchesPage(currentPath, firstStepDef.page)) {
        sessionStorage.setItem(PENDING_STEP_KEY, String(fromStep));
        await navigateToStep(firstStepDef, router);
        return;
      }

      // Collect the subset of steps that live on the current page,
      // starting from `fromStep`, skipping optional ones if element is absent.
      const pageSteps = stepsForPath(currentPath).filter(
        (s) => s.index >= fromStep,
      );

      // Filter out optional steps where the target element is missing.
      const visibleSteps = pageSteps.filter((s) => {
        if (!s.optional) return true;
        return !!document.querySelector(s.selector);
      });

      if (visibleSteps.length === 0) {
        // Nothing to show on this page - find the next page and navigate.
        const nextCrossPage = TOUR_STEPS.find(
          (s) => s.index >= fromStep && !matchesPage(currentPath, s.page),
        );
        if (nextCrossPage) {
          sessionStorage.setItem(PENDING_STEP_KEY, String(nextCrossPage.index));
          await navigateToStep(nextCrossPage, router);
        } else {
          // All done
          markCompleted();
        }
        return;
      }

      // Determine the last step index shown on this page so we know when
      // to cross-navigate to the next page.
      const lastVisibleIndex = visibleSteps[visibleSteps.length - 1].index;
      const nextCrossPageStep = TOUR_STEPS.find(
        (s) => s.index > lastVisibleIndex,
      );

      const driverSteps = visibleSteps.map((s, i) => {
        const isLastOnPage = i === visibleSteps.length - 1;
        return {
          element: s.selector,
          popover: {
            title: s.title,
            description: s.description,
            ...(allowCrossPage && isLastOnPage && nextCrossPageStep
              ? {
                nextBtnText: "Next →",
                onNextClick: () => {
                  driverInstance.destroy();
                  sessionStorage.setItem(
                    PENDING_STEP_KEY,
                    String(nextCrossPageStep.index),
                  );
                  navigateToStep(nextCrossPageStep, router);
                },
              }
              : {}),
          },
        };
      });

      const driverInstance = driver({
        showProgress: true,
        animate: true,
        overlayOpacity: 0.45,
        steps: driverSteps,
        onPopoverRender: (popover, { state }) => {
          // Don't show skip on the last step - "Done" already ends the tour.
          const isLast = state.activeIndex === driverSteps.length - 1;
          if (isLast) return;

          const skipBtn = document.createElement("button");
          skipBtn.textContent = "Skip tour";
          skipBtn.type = "button";
          skipBtn.className = "driver-skip-tour-btn";
          skipBtn.addEventListener("click", () => {
            driverInstance.destroy();
            markCompleted();
          });
          // Insert as first child so it sits on the far-left of the footer row.
          popover.footerButtons.insertBefore(skipBtn, popover.footerButtons.firstChild);
        },
        onDestroyStarted: () => {
          // If user explicitly closes (not via our onNextClick), mark done.
          driverInstance.destroy();
          markCompleted();
        },
      });

      driverInstance.drive();
    },
    [router, pathname],
  );

  const startTourFromPage = useCallback(() => {
    const stepsOnPage = stepsForPath(pathname);
    if (stepsOnPage.length > 0) {
      // Page-local tour: play steps for this page only, don't cross-navigate.
      void startTour(stepsOnPage[0].index, false);
    } else {
      // No steps defined for this page - fall back to the full global tour.
      void startTour(0, true);
    }
  }, [pathname, startTour]);

  return { startTour, startTourFromPage };
}

/** Fetch the most recent execution id for the execution-page steps. */
export async function getLatestExecutionId(): Promise<string | null> {
  try {
    const { listExecutions } = await import("@/lib/api");
    const result = await listExecutions({ page_size: 1 });
    return result.executions[0]?.id ?? null;
  } catch {
    return null;
  }
}

async function navigateToStep(
  step: TourStepDef,
  router: ReturnType<typeof useRouter>,
): Promise<void> {
  if (step.page.includes("/executions/")) {
    // Need a real execution id before navigating.
    const execId = await getLatestExecutionId();
    if (!execId) {
      // No executions yet - skip execution steps and look for the next one.
      const nextAfter = TOUR_STEPS.find((s) => s.index > step.index && !s.page.includes("/executions/"));
      if (nextAfter) {
        sessionStorage.setItem(PENDING_STEP_KEY, String(nextAfter.index));
        router.push(nextAfter.page);
      } else {
        markCompleted();
      }
      return;
    }
    router.push(`/dashboard/executions/${execId}`);
  } else {
    router.push(step.page);
  }
}

function markCompleted() {
  try {
    localStorage.setItem(COMPLETED_KEY, "true");
    // TODO: persist to backend once Better Auth user schema has
    // `has_completed_onboarding` boolean field on the user/tenant model.
  } catch {
    // localStorage may be unavailable in some contexts
  }
}

export { COMPLETED_KEY, PENDING_STEP_KEY, TOUR_STEPS, waitForElement };
