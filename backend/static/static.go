package static

import "embed"

// Frontend is the Next.js static export copied to this directory by
// frontend/scripts/sync-embed.cjs after `npm run build`.
//
//go:embed all:frontend
var Frontend embed.FS
