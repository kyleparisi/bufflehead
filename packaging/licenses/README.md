# Third-party distribution materials

Run `bin/collect-licenses --duckdb-source /path/to/duckdb-v1.1.3` for the target
platform before release. The collector uses the linked Go package graph, not
just direct go.mod requirements; it preserves nested license/NOTICE/copyright
files as well as the pinned DuckDB source, Godot's 4.6.1-stable LICENSE and
COPYRIGHT inventory, and Go runtime/vendor notices. It fails when a module
has no recognized license evidence. The current inventory is for macOS amd64;
regenerate and review for other targets or dependency changes.

`generated/THIRD-PARTY-NOTICES.txt` is embedded in the app's Help → Third-Party
Licenses window. `bin/bundle-licenses APP` copies it, the manifest and source
archives into `Contents/Resources/Licenses` before signing. The Developer ID
sign/notarize script invokes that step too. These materials do not change
Bufflehead's own license.

DuckDB v1.1.3 and its Go binding are MIT licensed; the exact copyright and
permission texts are preserved, together with native third-party notices.
Godot also has bundled third-party licenses beyond its own MIT license.
Source: https://duckdb.org/faq and https://godotengine.org/license/ . Godot
texts are pinned to https://github.com/godotengine/godot/tree/4.6.1-stable .

The unmodified MySQL Go driver is MPL-2.0. Its exact versioned module source
ZIP is distributed beside the notices; recipients can obtain the covered
source without having to request it. Preserve that source distribution and
MPL notices; if the driver is modified, replace the archive with corresponding
modified source. Do not substitute an unrelated repository's latest branch.

This is an evidence inventory and distribution implementation, not a claim
that automated filename collection proves complete compliance. Native builds,
additional extensions, asset changes and other-platform dependencies require
review against the final artifact. Some upstream inventories deliberately
include optional/unlinked components. No new third-party code was relicensed.

The modernc.org/libc testdata tree is excluded: it contains unlinked test-suite
licenses (including GPL text) and is not embedded by its production Go files.
This exclusion must be reassessed if production embeds or build inputs change.
