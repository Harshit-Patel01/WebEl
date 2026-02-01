package static

import "embed"

//go:embed frontend/* frontend/_next
var Frontend embed.FS
