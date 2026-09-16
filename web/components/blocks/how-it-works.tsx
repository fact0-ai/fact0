import { ArrowUpRight, Code2, TerminalSquare } from "lucide-react";
import { docsHref } from "@/lib/docs-origin";
import { CodePanel } from "./code-panel";
import { integrations, localSetup } from "./marketing-content";

export function HowItWorks() {
  return (
    <>
      <section className="retro-section" id="how-it-works">
        <div className="retro-container">
          <div className="retro-section-label"><span>[01] LOCAL FIRST</span><span>THREE SERVICES / ONE WORKSPACE</span></div>
          <div className="retro-setup-grid">
            <div>
              <h2 className="retro-section-title">Run it where<br />your data lives.</h2>
              <p className="retro-body">Start the API, dashboard and PostgreSQL with Docker Compose. Create the owner locally, make an API key, and point your agent at your installation.</p>
              <ul className="retro-checklist"><li>Owner password login</li><li>Authenticated ingestion and inspection</li><li>Data and signing keys stay with your instance</li></ul>
              <a className="retro-text-link" href={docsHref("quickstart")}>FULL INSTALLATION GUIDE <ArrowUpRight size={16} /></a>
            </div>
            <CodePanel code={localSetup} filename="01-start-fact0.sh" caption="Linux or macOS · Docker Compose v2 · Python 3 · OpenSSL 1.1.1+" />
          </div>
        </div>
      </section>
      <section className="retro-section" id="integrations">
        <div className="retro-container">
          <div className="retro-section-label"><span>[02] PICK YOUR ENTRY POINT</span><span>PYTHON + CLAUDE CODE</span></div>
          <h2 className="retro-section-title">Two ways in.<br /><span className="retro-underline">One history.</span></h2>
          <div className="retro-integrations">
            {integrations.map((item, index) => {
              const Icon = index === 0 ? Code2 : TerminalSquare;
              return <article className="retro-integration" key={item.id}>
                <div className="retro-integration-copy">
                  <p className="retro-tag"><Icon size={17} />{item.label}</p>
                  <h3>{item.title}</h3><p className="retro-body">{item.description}</p>
                  <a className="retro-text-link" href={docsHref(item.docsPath)}>OPEN THE GUIDE <ArrowUpRight size={16} /></a>
                </div>
                <CodePanel code={item.code} filename={item.filename} caption={item.note} />
              </article>;
            })}
          </div>
        </div>
      </section>
    </>
  );
}
