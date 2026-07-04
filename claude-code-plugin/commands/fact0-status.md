---
description: Show Fact0 capture status — whether session audit/telemetry is active and configured.
---

The Fact0 collector status is:

!`${CLAUDE_PLUGIN_ROOT}/bin/fact0-cc status`

Display the collector status output above to the user verbatim, then add a single
one-line summary stating whether **Fact0 capture is active** for this session.

Treat capture as **active** when the status indicates the collector is enabled and
an API key is configured (e.g. `FACT0_API_KEY` set, not disabled). Treat it as
**inactive** when `FACT0_CC_DISABLED=1`, the API key is missing, or the binary
reports it is not configured. If the command produced no output, report that the
collector binary returned nothing and capture is likely inactive (key unset or
disabled), and point the user at the `FACT0_API_KEY` / `FACT0_CC_DISABLED`
environment variables in the plugin README.
