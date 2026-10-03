#!/usr/bin/env node
/**
 * Copy Next.js static export (frontend/out) into backend/static/frontend
 * for Go embed. Used by `npm run build` on Windows, macOS, and Linux.
 */
const fs = require("fs");
const path = require("path");

const frontendRoot = path.join(__dirname, "..");
const src = path.join(frontendRoot, "out");
const dest = path.join(frontendRoot, "..", "backend", "static", "frontend");

if (!fs.existsSync(path.join(src, "index.html"))) {
  console.error(
    "sync-embed: missing frontend/out/index.html — next build (output: export) did not produce an export"
  );
  process.exit(1);
}

fs.rmSync(dest, { recursive: true, force: true });
fs.mkdirSync(dest, { recursive: true });
fs.cpSync(src, dest, { recursive: true });
console.log("sync-embed: copied frontend/out -> backend/static/frontend");
