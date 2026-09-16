import type { DAGEdge, DAGNode } from "./types";

/**
 * computeCriticalPath finds the longest end-to-end path through the
 * parent_child DAG by cumulative span duration.
 *
 * Each span's "weight" is its duration_ms. The path with the largest
 * sum is returned as a Set of span ids - the UI uses this for the
 * amber highlight on both the DAG and the waterfall.
 *
 * Causal edges are ignored: they're informational overlays, not part
 * of the timing hierarchy.
 */
export function computeCriticalPath(
  nodes: DAGNode[],
  edges: DAGEdge[],
): Set<string> {
  if (nodes.length === 0) return new Set();
  const duration = new Map<string, number>();
  nodes.forEach((n) => duration.set(n.span_id, Math.max(0, n.duration_ms)));

  const children = new Map<string, string[]>();
  const incoming = new Map<string, number>();
  nodes.forEach((n) => incoming.set(n.span_id, 0));
  edges
    .filter((e) => e.edge_type === "parent_child")
    .forEach((e) => {
      const c = children.get(e.source) || [];
      c.push(e.target);
      children.set(e.source, c);
      incoming.set(e.target, (incoming.get(e.target) ?? 0) + 1);
    });

  // Topological sort (Kahn). Stops gracefully if cycles exist -
  // unreachable nodes just keep their own duration as best score.
  const order: string[] = [];
  const queue: string[] = [];
  incoming.forEach((deg, id) => {
    if (deg === 0) queue.push(id);
  });
  while (queue.length) {
    const id = queue.shift()!;
    order.push(id);
    for (const c of children.get(id) ?? []) {
      const next = (incoming.get(c) ?? 0) - 1;
      incoming.set(c, next);
      if (next === 0) queue.push(c);
    }
  }
  // Append any unsorted nodes so they still contribute to the search.
  nodes.forEach((n) => {
    if (!order.includes(n.span_id)) order.push(n.span_id);
  });

  // DP: best[id] = max cumulative duration ending at id.
  const best = new Map<string, number>();
  const prev = new Map<string, string | null>();
  nodes.forEach((n) => {
    best.set(n.span_id, duration.get(n.span_id) ?? 0);
    prev.set(n.span_id, null);
  });
  // Reverse parent lookup: for each child, find its parents and take max(parent.best) + own.
  const parents = new Map<string, string[]>();
  edges
    .filter((e) => e.edge_type === "parent_child")
    .forEach((e) => {
      const p = parents.get(e.target) || [];
      p.push(e.source);
      parents.set(e.target, p);
    });
  for (const id of order) {
    const own = duration.get(id) ?? 0;
    let bestParent: string | null = null;
    let bestSum = 0;
    for (const p of parents.get(id) ?? []) {
      const ps = best.get(p) ?? 0;
      if (ps > bestSum) {
        bestSum = ps;
        bestParent = p;
      }
    }
    if (bestParent !== null) {
      best.set(id, bestSum + own);
      prev.set(id, bestParent);
    }
  }

  // Find the node with the maximum best - that's the critical-path tail.
  let tail: string | null = null;
  let max = -1;
  best.forEach((v, k) => {
    if (v > max) {
      max = v;
      tail = k;
    }
  });
  const path = new Set<string>();
  let cur: string | null = tail;
  while (cur) {
    if (path.has(cur)) break; // safety against cycles
    path.add(cur);
    cur = prev.get(cur) ?? null;
  }
  return path;
}
