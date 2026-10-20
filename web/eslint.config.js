import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    languageOptions: { globals: globals.browser },
    rules: {
      // Render output and every chain string is untrusted text
      // (docs/design.md §10.3): nothing is ever injected as HTML.
      "no-restricted-syntax": [
        "error",
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: "untrusted text is never rendered as HTML (design §10.3)",
        },
        {
          selector: "CallExpression[callee.name='Number'] > Identifier",
          message: "amounts are bigint or decimal strings, never Number (design §1)",
        },
      ],
    },
  },
);
