"use client";

import {
  BookOpen,
  ChevronRight,
  Clock,
  Code2,
  Copy,
  Cpu,
  Plus,
  Settings,
  Type
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

import { DashboardSkeleton } from "@/components/dashboard/dashboard-skeleton";
import { PageGetStarted } from "@/components/dashboard/page-get-started";
import { PageToolbar } from "@/components/dashboard/page-toolbar";
import { docsHref } from "@/lib/docs-origin";
import { useCreatePrompt, useListPrompts, usePromptDetail } from "@/lib/use-analytics";
import { useTour } from "@/lib/use-tour";
import { cn } from "@/lib/utils";

export default function PromptsPage() {
  const [selectedPromptID, setSelectedPromptID] = useState<string>("pmpt_01HZF1Z1Z1");
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  // Form State
  const [name, setName] = useState("");
  const [template, setTemplate] = useState("");
  const [variables, setVariables] = useState("");
  const [modelHints, setModelHints] = useState("");
  const [metadata, setMetadata] = useState("");

  const { startTourFromPage } = useTour();

  const helpContent = {
    title: "Prompt Registry",
    tagline: "Version-controlled prompt templates your agents fetch at runtime - no redeployment needed.",
    icon: <BookOpen className="size-4" />,
    concepts: [
      { title: "Prompt", body: "A named, versioned template. Agents call GET /v1/me/prompts/{name} to fetch the active version at runtime without changing code." },
      { title: "Versioning", body: "Each registration that changes the template creates a new version. Previous versions are preserved and auditable." },
      { title: "Usage Metrics", body: "Token averages and latency are computed from live spans that reference the prompt via prompt_name or prompt_id span metadata." },
    ],
    docHref: docsHref("observability/prompt-registry"),
    onTourStart: startTourFromPage,
  };

  const promptsState = useListPrompts();
  const detailState = usePromptDetail(selectedPromptID);
  const createPrompt = useCreatePrompt();

  const loading = promptsState.isLoading;
  const prompts = promptsState.data || [];
  const detail = detailState.data;

  const handleCopy = (text: string) => {
    void navigator.clipboard.writeText(text);
    setCopied(true);
    toast.success("Template copied to clipboard");
    setTimeout(() => setCopied(false), 2000);
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name || !template) {
      toast.error("Prompt name and template are required");
      return;
    }

    try {
      // Parse inputs
      const varsArr = variables.split(",").map(v => v.trim()).filter(Boolean);
      const modelsArr = modelHints.split(",").map(m => m.trim()).filter(Boolean);

      let metaObj: Record<string, string> = {};
      if (metadata.trim()) {
        try {
          metaObj = JSON.parse(metadata);
        } catch {
          toast.error("Metadata must be valid JSON");
          return;
        }
      }

      const res = await createPrompt.trigger({
        name,
        template,
        variables: varsArr,
        model_hints: modelsArr,
        metadata: metaObj,
      });

      toast.success(`Prompt "${name}" version registered successfully`);
      setIsCreateOpen(false);

      // Reset form
      setName("");
      setTemplate("");
      setVariables("");
      setModelHints("");
      setMetadata("");

      // Refresh listing
      void promptsState.mutate();
      if (res) {
        setSelectedPromptID(res.id);
      }
    } catch (err) {
      toast.error((err as Error).message || "Failed to register prompt version");
    }
  };

  return (
    <>
      <PageToolbar title="Prompt Registry" showFilters={false} helpContent={helpContent}>
        <button
          onClick={() => setIsCreateOpen(true)}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold text-white bg-indigo-600 hover:bg-indigo-700 shadow-sm transition-all"
        >
          <Plus className="size-4" />
          Register Prompt
        </button>
      </PageToolbar>

      <div className="animate-in fade-in slide-in-from-bottom-2 duration-500 ease-out p-6 lg:p-8">
        {loading ? (
          <DashboardSkeleton />
        ) : (
          <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 h-[calc(100vh-200px)] min-h-[500px]">

            {/* Left Pane: Prompts List (5 cols) */}
            <div data-tour="prompts-list" className="lg:col-span-5 border border-border/60 bg-card rounded-xl flex flex-col overflow-hidden h-full">
              <div className="px-4 py-3 border-b border-border/60 bg-muted/20 flex items-center justify-between">
                <span className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Registered Prompts</span>
                <span className="text-[10px] font-semibold font-mono text-zinc-400">{prompts.length} templates</span>
              </div>

              <div className="flex-1 overflow-y-auto divide-y divide-border/60">
                {prompts.length === 0 ? (
                  <PageGetStarted
                    icon={<BookOpen className="size-5 text-zinc-400" />}
                    title="No prompts registered yet"
                    description="The prompt catalog stores versioned templates your agents pull at runtime. Create a template above or push one via the SDK."
                    docLinks={[{ label: "Prompt Registry docs", href: docsHref("observability/prompt-registry") }]}
                    onTourStart={startTourFromPage}
                    className="border-0 rounded-none"
                  />
                ) : prompts.map((p) => {
                  const isSelected = p.id === selectedPromptID;
                  return (
                    <button
                      key={p.id}
                      onClick={() => setSelectedPromptID(p.id)}
                      className={cn(
                        "w-full text-left p-4 hover:bg-muted/40 transition-colors flex items-start justify-between relative",
                        isSelected && "bg-muted/50 border-r-2 border-indigo-500"
                      )}
                    >
                      <div className="space-y-1.5 min-w-0 pr-2">
                        <div className="flex items-center gap-2">
                          <span className="text-xs font-semibold text-foreground dark:text-white truncate font-mono">
                            {p.name}
                          </span>
                          <span className="px-1.5 py-0.5 rounded text-[8px] font-bold uppercase tracking-wider text-indigo-600 bg-indigo-500/10">
                            v{p.version}
                          </span>
                        </div>
                        <p className="text-[11px] text-zinc-500 truncate leading-normal">
                          {p.template}
                        </p>
                        <div className="flex items-center gap-3 text-[10px] text-zinc-400 font-medium">
                          <span className="flex items-center gap-1 font-mono">
                            <Plus className="size-3" />
                            {p.usage_count} uses
                          </span>
                          <span className="flex items-center gap-1 font-mono">
                            <Clock className="size-3" />
                            {p.avg_latency_ms}ms
                          </span>
                        </div>
                      </div>
                      <ChevronRight className={cn("size-4 text-zinc-400 shrink-0 mt-0.5", isSelected && "text-indigo-500")} />
                    </button>
                  );
                })}
              </div>
            </div>

            {/* Right Pane: Prompt Detail (7 cols) */}
            <div data-tour="prompt-detail" className="lg:col-span-7 border border-border/60 bg-card rounded-xl flex flex-col overflow-hidden h-full">
              {detailState.isLoading ? (
                <div className="flex-1 flex flex-col items-center justify-center space-y-3">
                  <div className="h-6 w-6 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin" />
                  <span className="text-xs text-zinc-500">Loading prompt details...</span>
                </div>
              ) : detail ? (
                <div className="flex-1 flex flex-col overflow-hidden">

                  {/* Detail Header */}
                  <div className="px-6 py-4 border-b border-border/60 bg-muted/20 flex flex-wrap items-center justify-between gap-4">
                    <div>
                      <div className="flex items-center gap-2">
                        <h3 className="text-sm font-bold text-foreground dark:text-white font-mono">
                          {detail.name}
                        </h3>
                        <span className="px-1.5 py-0.5 rounded text-[8px] font-bold uppercase tracking-wider text-indigo-600 bg-indigo-500/10">
                          Version {detail.version}
                        </span>
                      </div>
                      <div className="text-[10px] text-zinc-500 mt-0.5">
                        Registered {new Date(detail.created_at).toLocaleDateString()}
                      </div>
                    </div>

                    <div className="flex items-center gap-4 bg-background border border-border/60 px-3 py-1.5 rounded-lg text-xs font-medium text-zinc-600 dark:text-zinc-300 font-mono">
                      <div className="text-center">
                        <div className="text-[9px] uppercase tracking-wider text-zinc-500 font-sans font-bold">Calls</div>
                        <div className="mt-0.5 tabular-nums text-foreground dark:text-white">{detail.usage_count}</div>
                      </div>
                      <div className="h-4 w-px bg-border/60" />
                      <div className="text-center">
                        <div className="text-[9px] uppercase tracking-wider text-zinc-500 font-sans font-bold">Avg Tokens</div>
                        <div className="mt-0.5 tabular-nums text-foreground dark:text-white">{detail.avg_tokens || "-"}</div>
                      </div>
                      <div className="h-4 w-px bg-border/60" />
                      <div className="text-center">
                        <div className="text-[9px] uppercase tracking-wider text-zinc-500 font-sans font-bold">Avg Latency</div>
                        <div className="mt-0.5 tabular-nums text-foreground dark:text-white">{detail.avg_latency_ms}ms</div>
                      </div>
                    </div>
                  </div>

                  {/* Prompt Details Content */}
                  <div className="flex-1 overflow-y-auto p-6 space-y-5">

                    {/* Template Code Block */}
                    <div className="space-y-1.5">
                      <div className="flex items-center justify-between">
                        <span className="text-[9px] font-bold uppercase tracking-wider text-zinc-500 flex items-center gap-1.5">
                          <Code2 className="size-3.5 text-indigo-500" />
                          Template Body
                        </span>
                        <button
                          onClick={() => handleCopy(detail.template)}
                          className="inline-flex items-center gap-1 text-[10px] font-semibold text-zinc-500 hover:text-indigo-500 dark:hover:text-indigo-400 transition-colors"
                        >
                          <Copy className="size-3" />
                          {copied ? "Copied" : "Copy"}
                        </button>
                      </div>
                      <pre className="rounded-xl border border-border/60 bg-muted/30 p-4 text-xs font-mono text-foreground dark:text-white overflow-x-auto whitespace-pre-wrap leading-relaxed">
                        {detail.template}
                      </pre>
                    </div>

                    {/* Template Variables */}
                    {detail.variables && detail.variables.length > 0 && (
                      <div className="space-y-1.5">
                        <span className="text-[9px] font-bold uppercase tracking-wider text-zinc-500 flex items-center gap-1.5">
                          <Type className="size-3.5 text-emerald-500" />
                          Variables
                        </span>
                        <div className="flex flex-wrap gap-1.5">
                          {detail.variables.map((v) => (
                            <span key={v} className="px-2 py-0.5 rounded-md text-[10px] font-mono font-semibold text-emerald-600 bg-emerald-500/10 border border-emerald-500/20">
                              {"{{"}{v}{"}}"}
                            </span>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Model Hints */}
                    {detail.model_hints && detail.model_hints.length > 0 && (
                      <div className="space-y-1.5">
                        <span className="text-[9px] font-bold uppercase tracking-wider text-zinc-500 flex items-center gap-1.5">
                          <Cpu className="size-3.5 text-violet-500" />
                          Recommended Models
                        </span>
                        <div className="flex flex-wrap gap-1.5">
                          {detail.model_hints.map((m) => (
                            <span key={m} className="px-2 py-0.5 rounded-md text-[10px] font-mono font-semibold text-violet-600 bg-violet-500/10 border border-violet-500/20">
                              {m}
                            </span>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Metadata */}
                    {detail.metadata && Object.keys(detail.metadata).length > 0 && (
                      <div className="space-y-1.5">
                        <span className="text-[9px] font-bold uppercase tracking-wider text-zinc-500 flex items-center gap-1.5">
                          <Settings className="size-3.5 text-zinc-500" />
                          Metadata Attributes
                        </span>
                        <pre className="rounded-xl border border-border/60 bg-muted/20 p-3 text-[11px] font-mono text-zinc-600 dark:text-zinc-300">
                          {JSON.stringify(detail.metadata, null, 2)}
                        </pre>
                      </div>
                    )}

                  </div>

                </div>
              ) : (
                <div className="flex-1 flex flex-col items-center justify-center text-zinc-500 text-xs">
                  <BookOpen className="size-8 text-zinc-400 mb-2" />
                  Select a template to view details
                </div>
              )}
            </div>

          </div>
        )}
      </div>

      {/* Register Prompt Dialog Modal */}
      {isCreateOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-xs animate-in fade-in duration-200">
          <div className="bg-card border border-border/60 rounded-xl w-full max-w-lg overflow-hidden shadow-2xl p-6 space-y-4 animate-in zoom-in-95 duration-200">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-bold text-foreground dark:text-white">Register Prompt Configuration</h3>
              <button
                onClick={() => setIsCreateOpen(false)}
                className="text-xs font-semibold text-zinc-400 hover:text-foreground"
              >
                Close
              </button>
            </div>

            <form onSubmit={handleCreate} className="space-y-3.5">
              <div className="space-y-1">
                <label className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Prompt Name</label>
                <input
                  type="text"
                  required
                  placeholder="e.g. customer_greeting_email"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-border/60 bg-background text-xs text-foreground placeholder-zinc-500 focus:outline-none focus:border-indigo-500"
                />
              </div>

              <div className="space-y-1">
                <label className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Template Template Body</label>
                <textarea
                  required
                  rows={5}
                  placeholder="Hello {{name}}, Welcome to {{company}}!"
                  value={template}
                  onChange={(e) => setTemplate(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-border/60 bg-background text-xs font-mono text-foreground placeholder-zinc-500 focus:outline-none focus:border-indigo-500 leading-relaxed"
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div className="space-y-1">
                  <label className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Variables (comma separated)</label>
                  <input
                    type="text"
                    placeholder="name, company"
                    value={variables}
                    onChange={(e) => setVariables(e.target.value)}
                    className="w-full px-3 py-2 rounded-lg border border-border/60 bg-background text-xs text-foreground placeholder-zinc-500 focus:outline-none focus:border-indigo-500"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Model Hints (comma separated)</label>
                  <input
                    type="text"
                    placeholder="claude-3-5-sonnet, gpt-4o"
                    value={modelHints}
                    onChange={(e) => setModelHints(e.target.value)}
                    className="w-full px-3 py-2 rounded-lg border border-border/60 bg-background text-xs text-foreground placeholder-zinc-500 focus:outline-none focus:border-indigo-500"
                  />
                </div>
              </div>

              <div className="space-y-1">
                <label className="text-[10px] font-bold uppercase tracking-wider text-zinc-500">Metadata (JSON format)</label>
                <input
                  type="text"
                  placeholder='{"domain": "marketing", "stage": "onboarding"}'
                  value={metadata}
                  onChange={(e) => setMetadata(e.target.value)}
                  className="w-full px-3 py-2 rounded-lg border border-border/60 bg-background text-xs font-mono text-foreground placeholder-zinc-500 focus:outline-none focus:border-indigo-500"
                />
              </div>

              <div className="pt-2 flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setIsCreateOpen(false)}
                  className="px-3 py-2 rounded-lg border border-border/60 hover:bg-muted text-xs font-semibold text-zinc-500 transition-colors"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={createPrompt.isMutating}
                  className="px-3 py-2 rounded-lg text-white bg-indigo-600 hover:bg-indigo-700 text-xs font-semibold shadow-sm transition-all"
                >
                  {createPrompt.isMutating ? "Registering..." : "Submit Registration"}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  );
}
