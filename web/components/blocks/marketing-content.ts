export const heroFeatures = [
  {
    title: "Execution history",
    description: "Follow recorded steps, inspect inputs and outputs, and see where a run failed.",
  },
  {
    title: "Hash-chained audit records",
    description: "Check the consistency of stored events. Reported capture gaps remain visible and separate.",
  },
  {
    title: "Signed exports",
    description: "Download authenticated PDF reports and evidence ZIPs. Verify the signed files inside the ZIP.",
  },
] as const;

export const localSetup = `git clone https://github.com/fact0-ai/fact0.git
cd fact0
python3 scripts/init-env.py
docker compose up --build -d --wait
docker compose exec web node scripts/owner.mjs create`;

export const integrations = [
  {
    id: "python",
    label: "Python agents",
    title: "Instrument the agent you already run.",
    description: "Use the Python SDK to record execution spans, tool results, supported model content and audit events. Start with the included example; it needs no model account.",
    code: `python3 -m venv .venv
. .venv/bin/activate
pip install -e ./sdk/python
export FACT0_BASE_URL=http://localhost:8000
# Set FACT0_API_KEY to your local key.
python examples/local-python.py`,
    filename: "python-example.sh",
    docsPath: "sdk/python/installation",
    note: "The example includes a nested execution, a successful tool and a handled failure.",
  },
  {
    id: "claude",
    label: "Claude Code",
    title: "Keep a record of your coding sessions.",
    description: "Prepare the collector, connect the plugin and inspect supported prompts, assistant text, tool calls and errors. Claude keeps using your existing model configuration.",
    code: `export FACT0_BASE_URL=http://localhost:8000
# Set FACT0_API_KEY to your local key.
claude-code-plugin/bin/fact0-cc prepare
claude --plugin-dir ./claude-code-plugin`,
    filename: "claude-setup.sh",
    docsPath: "integrations/claude-code",
    note: "Raw capture is the default. Missing or unsupported content is marked partial or unavailable.",
  },
] as const;

export const inspectionFeatures = [
  {
    id: "execution",
    title: "Follow the execution.",
    description: "Move between the graph, timing waterfall and recorded spans to understand the order of a run.",
    docsPath: "concepts/executions",
  },
  {
    id: "payload",
    title: "Read the captured values.",
    description: "Expand supported prompts, assistant text, tool input/output and errors. Copy or download the full stored value.",
    docsPath: "integrations/claude-code#capture-modes",
  },
  {
    id: "replay",
    title: "Walk through the record.",
    description: "Replay saved execution events at your own pace. Replay reconstructs history; it does not rerun the agent.",
    docsPath: "sdk/python/telemetry",
  },
] as const;

export const faqCategories = [
  {
    title: "Getting started",
    questions: [
      {
        question: "What do I need to run Fact0?",
        answer: "Docker with Compose v2, Python 3 and OpenSSL 1.1.1 or newer on Linux or macOS. The core runs a Go API, Next.js dashboard and PostgreSQL. Setup creates one owner and one workspace; no Fact0 cloud account is required.",
      },
      {
        question: "Can I try it without a model account?",
        answer: "Yes. The included Python example records a synthetic execution with a tool call and handled failure. Claude Code capture uses your existing Claude installation and model configuration.",
      },
    ],
  },
  {
    title: "Capture and control",
    questions: [
      {
        question: "Where does captured data go?",
        answer: "To the PostgreSQL database in your installation. The Claude collector also keeps a private local retry spool. Raw capture can contain prompts, source code, personal data and secrets, so the operator controls access, backups and retention.",
      },
      {
        question: "Does it capture every agent action?",
        answer: "No. Python capture depends on your instrumentation. Claude capture depends on supported hooks and transcript formats. Missing, malformed or unavailable content is reported explicitly. Metadata and hash modes reduce content sent to the API; the private retry spool can still contain raw hook data. These modes are not secret-redaction guarantees.",
      },
    ],
  },
  {
    title: "Scope and trust",
    questions: [
      {
        question: "What does audit verification prove?",
        answer: "A passing check establishes consistency of the stored records checked. Export verification checks signed file bytes against a separately trusted public key. Neither proves complete capture, truthful source data or protection from an administrator replacing the whole history.",
      },
      {
        question: "Is this a hosted service or an enforcement tool?",
        answer: "This is experimental, MIT-licensed self-hosted software with best-effort maintenance. It does not provide hosted billing, team invitations, policy enforcement, a compliance certification or an SLA. The current supported release is for one owner and one workspace.",
      },
    ],
  },
] as const;
