// renderTemplate.js — turns a saved template's Puck definition + one
// invoice's real data into a complete HTML document, via Puck's own
// Render component (the exact same rendering logic the builder page's
// live canvas uses) run server-side through React's static-markup
// renderer. See scripts/sync-template-config.mjs's own doc comment for
// why this imports the *generated* copy of the shared component config
// rather than web/'s own file directly (a React module-singleton issue,
// confirmed empirically while wiring this up — not a style preference).
//
// Explicit React import: tsx/esbuild's tsconfig-based automatic-JSX-
// runtime detection proved unreliable for this project during testing
// (see scripts/sync-template-config.mjs's own doc comment for the
// related cross-project React-singleton issue this project already
// works around) — importing React directly here, and letting the JSX
// below compile to explicit React.createElement calls, sidesteps that
// uncertainty entirely rather than depending on it.
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Render } from "@puckeditor/core";
import { templateBuilderConfig, InvoiceDataProvider } from "./generated/templateBuilderConfig.tsx";

/**
 * renderTemplateToHtml builds a complete, self-contained HTML document
 * (inline CSS, no external assets — matching classicTemplate.js's own
 * approach) from a template's saved definition and one invoice's real
 * data (already shaped as InvoiceRenderData — see
 * generated/templateBuilderConfig.tsx — the renderer service's own
 * POST /render body is already exactly this shape, field-for-field,
 * plus the `definition` field this function itself consumes; see
 * internal/renderer.RenderRequest for the Go-side mirror).
 * @param {object} definition
 * @param {import("./generated/templateBuilderConfig.tsx").InvoiceRenderData} invoiceData
 * @returns {string}
 */
export function renderTemplateToHtml(definition, invoiceData) {
  const markup = renderToStaticMarkup(
    <InvoiceDataProvider value={invoiceData}>
      <Render config={templateBuilderConfig} data={definition} />
    </InvoiceDataProvider>,
  );

  return `<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<style>
  @page { size: A4; margin: 40px; }
  body { font-family: system-ui, -apple-system, sans-serif; font-size: 10pt; color: #111; margin: 0; }
  table { width: 100%; }
</style>
</head>
<body>${markup}</body>
</html>`;
}
