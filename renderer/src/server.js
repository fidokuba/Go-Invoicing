// server.js — the renderer service's HTTP surface. Internal-only: this
// is never meant to be reachable from outside the deployment (no auth,
// no tenant awareness, no rate limiting of its own) — the Go API is the
// only intended caller, the same way it's the only caller of PostgreSQL.
// Mirrors a few conventions from the Go API deliberately, so this
// service doesn't feel like a foreign piece of the system: a /health
// endpoint, a request body size limit, and graceful shutdown.

import express from "express";
import puppeteer from "puppeteer";
import { renderClassicTemplate } from "./classicTemplate.js";
import { renderTemplateToHtml } from "./renderTemplate.jsx";

const PORT = process.env.PORT || 3000;

// MAX_REQUEST_BODY_BYTES mirrors internal/httpx.MaxRequestBodyBytes (2
// MiB) — an invoice's rendering payload is line items and formatted
// strings, the same order of magnitude as the JSON bodies the main API
// already bounds at this same limit.
const MAX_REQUEST_BODY_BYTES = 2 * 1024 * 1024;

// RENDER_TIMEOUT_MS bounds a single PDF generation — generous for even a
// large multi-page invoice, but short enough that one stuck render can't
// tie up a worker indefinitely (see internal/config's own
// serverWriteTimeout for the same reasoning on the Go side).
const RENDER_TIMEOUT_MS = 15_000;

// MAX_CONCURRENT_RENDERS bounds how many Chromium pages this process
// ever has open at once (Hardening pass, custom invoice layouts). Each
// render is a real browser page/process tree — genuinely expensive in
// CPU and memory, unlike gopdf's in-process rendering — so an unbounded
// number of simultaneous requests could exhaust the container. A
// request beyond this limit is rejected immediately (503, see below),
// not queued: queueing risks an unbounded, slow-draining backlog behind
// a wall of pending renders, and internal/renderer.Client already has
// its own timeout — a fast, honest "try again shortly" beats a request
// that silently waits out most of that timeout in a queue.
const MAX_CONCURRENT_RENDERS = 4;
let activeRenders = 0;

const app = express();
app.use(express.json({ limit: MAX_REQUEST_BODY_BYTES }));

// One browser instance, reused across requests (a fresh page per
// request, never a fresh browser) — launching Chromium per-request would
// make every render pay startup cost; a single long-lived instance is
// the standard Puppeteer pattern for a server workload.
let browserPromise;
function getBrowser() {
  if (!browserPromise) {
    browserPromise = puppeteer.launch({
      headless: true,
      args: ["--no-sandbox", "--disable-setuid-sandbox"],
    });
  }
  return browserPromise;
}

app.get("/health", (_req, res) => {
  res.json({ status: "ok" });
});

// POST /render — body: a RenderRequest-shaped JSON payload (see
// internal/renderer/client.go's RenderRequest for the Go-side mirror of
// this exact shape). Response: application/pdf bytes, or a JSON error.
//
// A present, non-empty `definition` means a genuine user-created
// template (see that Go field's own doc comment) — rendered by
// interpreting it with Puck's own Render component (renderTemplate.jsx),
// the exact same logic the builder page's live canvas uses, fed this
// request's own fields as the real invoice data. Its absence means the
// system Classic template, which InvoicePDFService.Generate never
// actually routes here for in production (it renders Classic via gopdf
// instead — see that method's own doc comment) — this fallback exists
// for direct/manual calls to this endpoint (Phase 1's own proof, e.g.)
// and as a reasonable default, not because real traffic depends on it.
app.post("/render", async (req, res) => {
  // Capacity check first, before anything else costs a Chromium page —
  // rejected outright rather than queued (see MAX_CONCURRENT_RENDERS's
  // own comment for why). 503 + Retry-After is the standard HTTP idiom
  // for "capacity, not your fault, try again shortly", and it gives
  // internal/renderer.Client's caller a clear, fast signal to distinguish
  // from an actual render failure (500).
  if (activeRenders >= MAX_CONCURRENT_RENDERS) {
    res.set("Retry-After", "2");
    res.status(503).json({ error: { message: "renderer at capacity, try again shortly" } });
    return;
  }

  activeRenders++;
  let page;
  try {
    const body = req.body ?? {};
    const html = body.definition ? renderTemplateToHtml(body.definition, body) : renderClassicTemplate(body);

    const browser = await getBrowser();
    page = await browser.newPage();
    await page.setContent(html, { waitUntil: "load", timeout: RENDER_TIMEOUT_MS });

    const pdf = await page.pdf({
      format: "a4",
      printBackground: true,
      timeout: RENDER_TIMEOUT_MS,
    });

    // page.pdf() returns a Uint8Array, not a Node Buffer — express's
    // res.send only recognises a true Buffer as binary and otherwise
    // falls back to JSON-encoding it (as {"0":37,"1":80,...}), so this
    // wrap is required, not defensive styling.
    res.set("Content-Type", "application/pdf");
    res.send(Buffer.from(pdf));
  } catch (err) {
    console.error("render failed:", err);
    res.status(500).json({ error: { message: "render failed" } });
  } finally {
    activeRenders--;
    if (page) await page.close().catch(() => {});
  }
});

const server = app.listen(PORT, () => {
  console.log(`renderer listening on :${PORT}`);
});

// Graceful shutdown — closes the browser before exiting, mirroring
// cmd/api/main.go's own shutdown ordering (stop accepting new work,
// then release the expensive resource) rather than leaving a Chromium
// process orphaned on SIGTERM.
async function shutdown() {
  server.close();
  if (browserPromise) {
    const browser = await browserPromise;
    await browser.close();
  }
  process.exit(0);
}

process.on("SIGTERM", shutdown);
process.on("SIGINT", shutdown);
