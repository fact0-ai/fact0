import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";
import { defineConfig, globalIgnores } from "eslint/config";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    // Fumadocs-generated source tree - do not lint auto-generated files.
    ".source/**",
  ]),
  {
    rules: {
      // These two rules come from eslint-plugin-react-hooks 5.x (React Compiler
      // compatibility rules). They flag many valid, widely-used React patterns:
      //   • setMounted(true) in a one-time mount effect (hydration guard)
      //   • setState() in an effect that reacts to dependency changes
      //   • Date.now() in render for elapsed-time display
      // Downgrade to warnings so CI stays green while we incrementally adopt
      // the stricter React Compiler style.
      "react-hooks/set-state-in-effect": "warn",
      "react-hooks/purity": "warn",
      "react-hooks/refs": "warn",
    },
  },
]);

export default eslintConfig;
