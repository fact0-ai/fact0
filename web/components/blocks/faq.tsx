import { ArrowUpRight, Plus } from "lucide-react";
import { docsHref } from "@/lib/docs-origin";
import { faqCategories } from "./marketing-content";

export function FAQ() {
  return <section className="retro-section" id="faq"><div className="retro-container">
    <div className="retro-section-label"><span>[05] THE FINE PRINT</span><span>BEFORE YOU RUN IT</span></div>
    <div className="retro-faq-grid">
      <div><h2 className="retro-section-title">Good questions.<br />Plain answers.</h2><p className="retro-body">Experimental software, explicit limits. Read the source, inspect what is captured, and decide what belongs in your installation.</p><a className="retro-text-link" href={docsHref("guides/security")}>SECURITY + DATA HANDLING <ArrowUpRight size={16} /></a></div>
      <div className="retro-faq-groups">{faqCategories.map(category => <div className="retro-faq-group" key={category.title}><h3>{category.title}</h3>{category.questions.map(item => <details key={item.question}><summary>{item.question}<Plus size={18} aria-hidden="true" /></summary><p>{item.answer}</p></details>)}</div>)}</div>
    </div>
  </div></section>;
}
