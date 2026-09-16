"use client";

import type { DAGEdge, DAGNode, SpanKind } from "@/lib/types";
import { SPAN_TYPE_COLORS, STATUS_COLORS, formatDuration } from "@/lib/utils";
import dagre from "@dagrejs/dagre";
import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Edge,
  type Node,
  type NodeTypes,
} from "@xyflow/react";
import { useCallback, useEffect, useMemo } from "react";

// ─── Layout constants (must match SpanNode visual size) ───────────────────────

const NODE_WIDTH = 248;
const NODE_HEIGHT = 96;

// ─── Custom Node ─────────────────────────────────────────────────────────────

interface SpanNodeData {
  label: string;
  spanType: SpanKind;
  status: string;
  durationMs: number;
  spanId: string;
  isActive?: boolean;
  isCritical?: boolean;
  [key: string]: unknown;
}

function SpanNode({ data, selected }: { data: SpanNodeData; selected?: boolean }) {
  const statusColor = STATUS_COLORS[data.status] || "#6b7280";
  const typeColor = SPAN_TYPE_COLORS[data.spanType] || "#6b7280";

  return (
    <div
      className={[
        "relative flex overflow-hidden rounded-xl border bg-card shadow-md transition-all duration-200 cursor-pointer",
        "min-w-[248px] max-w-[280px]",
        selected
          ? "ring-2 ring-primary ring-offset-2 ring-offset-background border-primary/50 scale-[1.02]"
          : data.isActive
            ? "border-primary/60 shadow-[0_0_20px_rgba(79,70,229,0.3)]"
            : data.isCritical
              ? "border-amber-500/60 shadow-[0_0_16px_rgba(245,158,11,0.25)]"
              : "border-border hover:border-border/80 hover:shadow-lg",
      ].join(" ")}
    >
      <Handle
        type="target"
        position={Position.Top}
        className="!w-2.5 !h-2.5 !border-2 !border-card !bg-slate-400"
      />

      <div className="w-1 shrink-0" style={{ backgroundColor: typeColor }} />

      <div className="flex-1 px-4 py-3 min-w-0">
        <div
          className="absolute top-2.5 right-2.5 size-2.5 rounded-full border-2 border-card"
          style={{ backgroundColor: statusColor }}
          title={data.status}
        />

        <div
          className="text-[10px] font-mono uppercase tracking-widest font-bold mb-1.5"
          style={{ color: typeColor }}
        >
          {data.spanType.replace(/_/g, " ")}
        </div>

        <div
          className={[
            "text-[15px] font-semibold leading-snug truncate",
            data.isActive ? "text-primary" : "text-foreground",
          ].join(" ")}
          title={data.label}
        >
          {data.label}
        </div>

        {data.durationMs > 0 && (
          <div className="text-[11px] text-muted-foreground mt-1.5 font-mono tabular-nums">
            {formatDuration(data.durationMs)}
          </div>
        )}
      </div>

      <Handle
        type="source"
        position={Position.Bottom}
        className="!w-2.5 !h-2.5 !border-2 !border-card !bg-slate-400"
      />
    </div>
  );
}

const nodeTypes: NodeTypes = { spanNode: SpanNode };

// ─── DAG Component ────────────────────────────────────────────────────────────

interface ExecutionDAGProps {
  dagNodes: DAGNode[];
  dagEdges: DAGEdge[];
  onNodeClick?: (spanId: string) => void;
  selectedSpanId?: string | null;
  activeSpanIds?: Set<string>;
  criticalSpanIds?: Set<string>;
}

const EDGE_COLOR_PARENT = "#94a3b8";
const EDGE_COLOR_CAUSAL = "#a78bfa";
const EDGE_COLOR_ACTIVE = "#6366f1";
const EDGE_COLOR_CRITICAL = "#f59e0b";

function layoutConfig(nodeCount: number, parentEdgeCount: number) {
  // Shallow executions read better left-to-right on wide panels.
  const horizontal = nodeCount <= 8 || parentEdgeCount <= 6;
  return horizontal
    ? {
        rankdir: "LR" as const,
        ranksep: 72,
        nodesep: 56,
        marginx: 48,
        marginy: 48,
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
      }
    : {
        rankdir: "TB" as const,
        ranksep: 64,
        nodesep: 48,
        marginx: 40,
        marginy: 40,
        sourcePosition: Position.Bottom,
        targetPosition: Position.Top,
      };
}

function computeDagreLayout(
  dagNodes: DAGNode[],
  parentChildEdges: DAGEdge[],
): {
  positions: Map<string, { x: number; y: number }>;
  sourcePosition: Position;
  targetPosition: Position;
} {
  const { rankdir, ranksep, nodesep, marginx, marginy, sourcePosition, targetPosition } =
    layoutConfig(dagNodes.length, parentChildEdges.length);

  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir, ranksep, nodesep, marginx, marginy });
  g.setDefaultEdgeLabel(() => ({}));

  dagNodes.forEach((n) =>
    g.setNode(n.span_id, { width: NODE_WIDTH, height: NODE_HEIGHT }),
  );
  parentChildEdges.forEach((e) => {
    if (g.hasNode(e.source) && g.hasNode(e.target)) {
      g.setEdge(e.source, e.target);
    }
  });

  dagre.layout(g);

  const positions = new Map<string, { x: number; y: number }>();
  dagNodes.forEach((n) => {
    const pos = g.node(n.span_id);
    if (pos) {
      positions.set(n.span_id, {
        x: pos.x - NODE_WIDTH / 2,
        y: pos.y - NODE_HEIGHT / 2,
      });
    }
  });

  return { positions, sourcePosition, targetPosition };
}

/** Re-fit when the graph changes so small DAGs don't shrink into the center. */
function GraphViewportSync({ nodeCount }: { nodeCount: number }) {
  const { fitView } = useReactFlow();

  useEffect(() => {
    const timer = window.setTimeout(() => {
      if (nodeCount === 0) return;

      const compact = nodeCount <= 8;
      fitView({
        padding: compact ? 0.18 : 0.12,
        minZoom: compact ? 0.92 : 0.45,
        maxZoom: compact ? 1.35 : 1.15,
        duration: 220,
      });
    }, 60);

    return () => window.clearTimeout(timer);
  }, [nodeCount, fitView]);

  return null;
}

function ExecutionDAGCanvas({
  dagNodes,
  dagEdges,
  onNodeClick,
  selectedSpanId,
  activeSpanIds,
  criticalSpanIds,
}: ExecutionDAGProps) {
  const { initialNodes, initialEdges } = useMemo(() => {
    const parentChildEdges = dagEdges.filter((e) => e.edge_type === "parent_child");
    const { positions, sourcePosition, targetPosition } = computeDagreLayout(
      dagNodes,
      parentChildEdges,
    );

    const initialNodes: Node<SpanNodeData>[] = dagNodes.map((n, idx) => {
      const pos = positions.get(n.span_id) ?? {
        x: idx * (NODE_WIDTH + 80),
        y: 0,
      };
      return {
        id: n.span_id,
        type: "spanNode",
        position: pos,
        sourcePosition,
        targetPosition,
        data: {
          label: n.name,
          spanType: n.span_type,
          status: n.status,
          durationMs: n.duration_ms,
          spanId: n.span_id,
          isActive: false,
          isCritical: false,
        },
      };
    });

    const initialEdges: Edge[] = dagEdges.map((e, i) => ({
      id: `edge-${i}`,
      source: e.source,
      target: e.target,
      type: "smoothstep",
      animated: false,
      data: { edgeType: e.edge_type },
      style: {
        stroke: e.edge_type === "parent_child" ? EDGE_COLOR_PARENT : EDGE_COLOR_CAUSAL,
        strokeWidth: e.edge_type === "parent_child" ? 2.5 : 2,
        strokeDasharray: e.edge_type === "causal" ? "6 4" : undefined,
      },
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color: e.edge_type === "parent_child" ? EDGE_COLOR_PARENT : EDGE_COLOR_CAUSAL,
        width: 18,
        height: 18,
      },
    }));

    return { initialNodes, initialEdges };
  }, [dagNodes, dagEdges]);

  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);

  useEffect(() => {
    setNodes(initialNodes);
    setEdges(initialEdges);
  }, [initialNodes, initialEdges, setNodes, setEdges]);

  useEffect(() => {
    setNodes((nds) =>
      nds.map((n) => {
        const data = n.data as SpanNodeData;
        const isActive = activeSpanIds ? activeSpanIds.has(data.spanId) : false;
        const isCritical = criticalSpanIds ? criticalSpanIds.has(data.spanId) : false;
        const selected = selectedSpanId === data.spanId;
        if (
          data.isActive === isActive &&
          data.isCritical === isCritical &&
          n.selected === selected
        ) {
          return n;
        }
        return { ...n, selected, data: { ...data, isActive, isCritical } };
      }),
    );

    setEdges((eds) =>
      eds.map((e) => {
        const isActive = activeSpanIds ? activeSpanIds.has(e.target) : false;
        const isCritical =
          !!criticalSpanIds &&
          criticalSpanIds.has(e.source) &&
          criticalSpanIds.has(e.target);
        const edgeType = e.data?.edgeType as string;
        const color = isActive
          ? EDGE_COLOR_ACTIVE
          : isCritical
            ? EDGE_COLOR_CRITICAL
            : edgeType === "parent_child"
              ? EDGE_COLOR_PARENT
              : EDGE_COLOR_CAUSAL;
        return {
          ...e,
          animated: isActive,
          style: {
            ...e.style,
            stroke: color,
            strokeWidth: isActive || isCritical ? 3 : edgeType === "parent_child" ? 2.5 : 2,
          },
          markerEnd: {
            type: MarkerType.ArrowClosed,
            color,
            width: 18,
            height: 18,
          },
        };
      }),
    );
  }, [activeSpanIds, criticalSpanIds, selectedSpanId, setNodes, setEdges]);

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      onNodeClick?.((node.data as SpanNodeData).spanId);
    },
    [onNodeClick],
  );

  if (dagNodes.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
        No spans in this execution yet.
      </div>
    );
  }

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onNodeClick={handleNodeClick}
      nodeTypes={nodeTypes}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable
      panOnScroll
      zoomOnScroll
      minZoom={0.35}
      maxZoom={2}
      proOptions={{ hideAttribution: true }}
      colorMode="system"
    >
      <GraphViewportSync nodeCount={dagNodes.length} />
      <Background
        variant={BackgroundVariant.Dots}
        gap={24}
        size={1}
        className="opacity-30!"
      />
      <Controls className="[&>button]:bg-card! [&>button]:border-border! [&>button]:text-muted-foreground! [&>button:hover]:bg-muted! rounded-xl! border-border! shadow-sm!" />
      {dagNodes.length > 6 && (
        <MiniMap
          className="bg-card! border-border! rounded-xl! shadow-sm!"
          nodeColor={(n) =>
            (n.data as SpanNodeData).isActive ? "#6366f1" : "hsl(var(--muted))"
          }
          maskColor="rgba(0,0,0,0.08)"
        />
      )}
    </ReactFlow>
  );
}

export default function ExecutionDAG(props: ExecutionDAGProps) {
  return (
    <div className="h-full w-full bg-muted/15">
      <ReactFlowProvider>
        <ExecutionDAGCanvas {...props} />
      </ReactFlowProvider>
    </div>
  );
}
