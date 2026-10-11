// This file declares the embedded library/template-author agent guide pair:
// internal/scaffold/libraryguide/AGENTS_KALIDE.md (the full guide) and
// internal/scaffold/libraryguide/AGENTS.md (its pointer)
// (requirements.requirement.library-agent-guide).
//
// The guides live outside the seed/ tree so writeHelloSeed never copies them
// into a deck: they are the library author's counterpart to the deck-root
// AGENTS_KALIDE.md/AGENTS.md pair the seed carries. Like the seed they are
// static, authored files embedded here in internal/scaffold, unbranded and
// text-only, and they are not template assets. The embed declaration only; the
// read helpers that return their bytes are mustReadLibraryGuide (the AGENTS.md
// pointer) in library.go and mustReadLibraryFullGuide (the AGENTS_KALIDE.md
// full guide) in agentsguide.go, mirroring mustReadSeed.
package scaffold

import "embed"

// libraryGuideEmbed is the embedded library guide tree, rooted so that
// "libraryguide" is its direct entry.
//
//go:embed libraryguide/AGENTS.md libraryguide/AGENTS_KALIDE.md
var libraryGuideEmbed embed.FS

// libraryGuideRoot is the sub-tree of libraryGuideEmbed rooted at
// "libraryguide", resolved once: the embed directive above guarantees it
// exists, so resolving lazily would only add error handling that can never
// trigger at runtime.
var libraryGuideRoot = mustSeedSub(libraryGuideEmbed, "libraryguide")
