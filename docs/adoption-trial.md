# Five-person adoption trial

Test whether people with an existing inspection problem can install Fact0, find something useful and choose to return. Five participants cannot establish product-market fit.

## Participants and baseline

- [ ] Include **three current Claude Code users** and **two Python agent developers**, each using their own fresh local installation.
- [ ] Before showing Fact0, ask about one recent, actual inspection problem: what happened, what they needed to find, how they investigated, and what remained unresolved.
- [ ] Record frequency, approximate time spent and their existing alternative. General interest in AI does not qualify as inspection pain.
- [ ] Record OS/architecture, tool versions and release revision. Use the same released revision across the cohort; record updates.
- [ ] Make observation, notes and the day-seven conversation voluntary; participants can stop or decline questions.

Use pseudonyms; keep completed trackers outside the public repository. No raw prompts, code, transcripts, tool output, keys, credentials or exports in notes. No screen recording by default. Obtain consent for non-sensitive quotes and agree when notes will be discarded. Participants keep their own instance/data.

## First attempt: public docs, no coaching

Give participants the [quickstart](../README.md#quickstart) and [Claude Code](integrations/claude-code.mdx) or [Python](sdk/python/installation.mdx) guide. Ask them to install Fact0 and inspect a harmless example. No demonstration or private fixes beforehand; public docs and ordinary searches are allowed.

Start the clock; separate active effort from downloads/build wait. Observe quietly. If they request help or stop, record the milestone/blocker **before** helping; mark subsequent progress as assisted. Intervene before sensitive material is shared. Assisted completion is not an independent install.

Use synthetic data in a disposable project first. Claude capture and its local journal can store sensitive content; reduced outbound modes do not remove raw journal content. Review [capture/privacy details](../claude-code-plugin/README.md#capture-and-delivery). Later use on normal work is optional.

### Released command reference

Observe these public steps without coaching. Use the [documented alternate ports](../README.md#quickstart) if defaults are occupied.

```sh
git clone --branch core/v0.1.0 https://github.com/fact0-ai/fact0.git
cd fact0
python3 scripts/init-env.py
docker compose up --build -d --wait
docker compose exec web node scripts/owner.mjs create
```

Sign in at `http://localhost:3000`; create a key in Settings → API keys. Set `FACT0_API_KEY` privately, excluding it from notes/issues/shared shell history. From the repository root, both paths use:

```sh
export FACT0_BASE_URL=http://localhost:8000
```

**Claude Code:** prepare the published 0.3.0 collector using `curl` and `sha256sum` or `shasum`; no Go required.

```sh
claude-code-plugin/bin/fact0-cc prepare
claude --plugin-dir ./claude-code-plugin
```

Capture a successful tool call and a harmless intentional failure; end the session. In Coding Agents, find the prompt, assistant content, tool result/error and capture status. See the [plugin guide](../claude-code-plugin/README.md#inspect-and-retry) for `status`, `flush` and `verify`. Nonblocking hooks do not guarantee delivery.

**Python:** run the deterministic example, which needs no model account.

```sh
python3 -m venv .venv
. .venv/bin/activate
pip install -e ./sdk/python
python examples/local-python.py
```

In Executions, find the successful tool and handled failure. The example also verifies the chain and saves an evidence ZIP. Ask the participant to explain their finding.

**Both paths:** verify in Audit and download an evidence ZIP. Follow [verification and exports](guides/verification.mdx), obtaining the trusted public key separately with `python3 scripts/export-public-key.py`. Keep exports locally. Ask what passing means: recorded-chain consistency and signed-file integrity, not complete capture or a backup.

## Milestones and blank tracker

Copy this table privately for each pseudonym. Record elapsed and active/waiting minutes plus assistance. Leave unfinished milestones blank.

| Milestone | Done at / elapsed minutes | Active / waiting minutes | Help or blocker, without sensitive content |
| --- | --- | --- | --- |
| Clone completed | | | |
| Services ready | | | |
| Owner created and signed in | | | |
| Local API key configured | | | |
| First capture visible | | | |
| Success and failure inspected | | | |
| Audit verification understood | | | |
| Evidence ZIP downloaded and signature checked | | | |

| Pseudonym | Path | Prior inspection problem | Independent install? | Useful finding | Unprompted return by day 7? | Support minutes / blocker issue |
| --- | --- | --- | --- | --- | --- | --- |
| C1 | Claude Code | | | | | |
| C2 | Claude Code | | | | | |
| C3 | Claude Code | | | | | |
| P1 | Python | | | | | |
| P2 | Python | | | | | |

## Baseline comparison and day seven

After first use: “What could you determine?”, “How would you do that with existing tools?”, and “What was missing or more work?” Avoid suggesting a benefit.

At the voluntary day-seven conversation, ask:

- Did you open Fact0 again before this follow-up? What triggered it, and what did you actually inspect?
- Did it resolve any part of the original problem? What changed in effort, clarity or outcome compared with your baseline?
- If you did not return, what did you use instead, or was there no inspection need this week?
- What one change would remove the biggest obstacle? Would you keep the installation, and why?

A survey visit, reminder-driven demo or assisted rerun is not an unprompted return. Record “no opportunity” separately from rejection or success.

## Decision and sustainable follow-through

Look for **three independent installs**, **two unprompted returns within seven days**, and **one specific inspection problem helped**. Independent means first capture and inspection without live coaching; useful means a concrete participant account tied to the baseline, not a compliment. These are heuristics for another small cohort, not product-market-fit proof. Publish only consented aggregates, disclosing sample size and assistance.

Convert blockers into small issues: milestone, version/OS, expected/observed behavior, synthetic reproduction, assistance and a completion check. Obtain consent before publishing sanitized reports. Deduplicate; prioritize blockers to capture, inspection and trustworthy verification before new features.

Set an affordable support/triage budget beforehand; record actual minutes and keep a small next-issue list. After five participants, continue, narrow scope or pause based on use and maintenance cost. Preserve [best-effort maintenance](../CONTRIBUTING.md), without response-time promises. Share [operation/shutdown](guides/self-hosting.mdx) so participants can stop services without deleting data.
