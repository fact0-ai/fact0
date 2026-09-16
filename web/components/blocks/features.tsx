import { ArrowUpRight } from "lucide-react";
import { docsHref } from "@/lib/docs-origin";
import { inspectionFeatures } from "./marketing-content";

function Diagram({ type }: { type: string }) {
  if (type === "execution") return <div className="retro-mini-graph" aria-hidden="true">
    <span className="retro-graph-node">run.start</span><span className="retro-graph-stem" />
    <div className="retro-graph-branches"><span className="retro-graph-node">tool.call</span><span className="retro-graph-node retro-node-coral">handled_error</span></div>
    <span className="retro-graph-stem" /><span className="retro-graph-node">run.end</span>
  </div>;
  if (type === "payload") return <div className="retro-mini-payload" aria-hidden="true">
    <div><span>TOOL INPUT / RAW</span><span>[+]</span></div>
    <pre>{`{\n  "tool": "Read",\n  "path": "src/agent.py",\n  "result": "..."\n}`}</pre>
    <div><span>CAPTURE STATUS</span><strong>PARTIAL</strong></div>
  </div>;
  return <div className="retro-mini-timeline" aria-hidden="true">
    <div className="retro-timeline-axis"><span>0ms</span><span>500ms</span></div>
    {["prompt", "tool", "response"].map((name, i) => <div className="retro-timeline-row" key={name}><span>{name}</span><i style={{ marginLeft: `${i * 12}%`, width: `${62 - i * 15}%` }} /></div>)}
    <div className="retro-timeline-controls"><span>◀</span><span>▶</span><div /><span>3 / 8</span></div>
  </div>;
}

export function Features() {
  return (
    <>
      <section className="retro-section" id="inspect">
        <div className="retro-container">
          <div className="retro-section-label"><span>[03] INSPECT THE DETAILS</span><span>ILLUSTRATED VIEWS</span></div>
          <div className="retro-section-intro"><h2 className="retro-section-title">Less guessing.<br />More of the record.</h2><p className="retro-body">A graph for the path. A timeline for the order. The captured content beside the step that produced it.</p></div>
          <div className="retro-feature-grid">
            {inspectionFeatures.map((item, index) => <article className="retro-feature" key={item.id}>
              <div className="retro-feature-visual"><span className="retro-visual-label">VIEW_0{index + 1}</span><Diagram type={item.id} /></div>
              <div className="retro-feature-copy"><h3>{item.title}</h3><p>{item.description}</p><a className="retro-text-link" href={docsHref(item.docsPath)}>READ MORE <ArrowUpRight size={15} /></a></div>
            </article>)}
          </div>
        </div>
      </section>
      <section className="retro-section" id="audit">
        <div className="retro-container">
          <div className="retro-section-label"><span>[04] CHECK THE HISTORY</span><span>WITH THE LIMITS IN VIEW</span></div>
          <div className="retro-section-intro"><h2 className="retro-section-title">Verify the record.<br />Know what it means.</h2><p className="retro-body">Audit verification and signed exports make stored history inspectable. They do not turn missing capture into complete evidence.</p></div>
          <div className="retro-table-wrap"><table className="retro-table"><thead><tr><th>CHECK</th><th>WHAT YOU CAN ESTABLISH</th></tr></thead><tbody>
            <tr><th>Audit chain</th><td>The checked records are consistent with their hashes and preceding links.</td></tr>
            <tr><th>Signed files in the evidence ZIP</th><td>The PDF and verification JSON match a separately trusted instance public key. The ZIP archive itself is not signed.</td></tr>
            <tr><th>Capture status</th><td>Supported omissions are shown as partial or unavailable, with a reason.</td></tr>
            <tr className="retro-table-note"><th>Outside that guarantee</th><td>Complete capture, truthful source data, regulatory compliance or protection from an administrator replacing the whole history.</td></tr>
          </tbody></table></div>
          <a className="retro-text-link retro-table-link" href={docsHref("guides/verification")}>VERIFICATION + EXPORT GUIDE <ArrowUpRight size={16} /></a>
        </div>
      </section>
    </>
  );
}
