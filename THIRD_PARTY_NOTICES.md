# Third-party notices

Fact0 source is MIT licensed (LICENSE). Dependencies retain their own licenses; the Go and npm lockfiles identify the included versions. The API image includes `api/THIRD_PARTY_LICENSES.txt` at `/usr/share/licenses/fact0/`; regenerate it with `python3 scripts/dependency-notices.py` after dependency changes. The web image retains dependency notices in its `node_modules` packages and places the application and font licenses in `/usr/share/licenses/fact0/`.

## DM Sans

The local DM Sans font files in `web/fonts/dm-sans/` are distributed under the SIL Open Font License 1.1. The complete notice accompanies the files as `OFL.txt`; upstream source is https://github.com/google/fonts/tree/main/ofl/dmsans.

The bundled static fonts retain their embedded copyright notice: Copyright 2014–2017 Indian Type Foundry (info@indiantypefoundry.com), with Reserved Font Name “Poppins”; Copyright 2019 Google LLC. The current upstream license also credits The DM Sans Project Authors. The font binaries are redistributed unchanged.

Third-party trademarks do not imply endorsement or customer relationships. Promotional customer/testimonial assets are not part of the supported product claims.
