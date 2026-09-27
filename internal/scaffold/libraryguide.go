// This file declares the embedded library/template-author agent guide:
// internal/scaffold/libraryguide/AGENTS.md
// (requirements.requirement.library-agent-guide).
//
// The guide lives outside the seed/ tree so writeHelloSeed never copies it
// into a deck: it is the library author's counterpart to the deck-root
// AGENTS.md the seed carries. Like the seed it is a static, authored file
// embedded here in internal/scaffold, unbranded and text-only, and it is not
// a template asset. The embed declaration only; the read helper that returns
// its bytes is mustReadLibraryGuide in library.go, mirroring mustReadSeed.
package scaffold

import "embed"

// libraryGuideEmbed is the embedded library guide tree, rooted so that
// "libraryguide" is its direct entry.
//
//go:embed libraryguide/AGENTS.md
var libraryGuideEmbed embed.FS

// libraryGuideRoot is the sub-tree of libraryGuideEmbed rooted at
// "libraryguide", resolved once: the embed directive above guarantees it
// exists, so resolving lazily would only add error handling that can never
// trigger at runtime.
var libraryGuideRoot = mustSeedSub(libraryGuideEmbed, "libraryguide")
