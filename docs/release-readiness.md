# First release candidate: v0.1.0

The first release candidate is prepared for the root module and the optional Bleve module. Publication remains a separate step. Neither module has a published tag yet.

| Module | Candidate tag | Minimum Go |
| --- | --- | --- |
| `github.com/goforj/search` | v0.1.0 | 1.24 |
| `github.com/goforj/search/driver/blevesearch` | driver/blevesearch/v0.1.0 | 1.25 |

The embedded module's root requirement, tooling requirements, and workspace replacements are aligned to v0.1.0. Published modules contain no local replacements. The docs, examples, and integration modules retain their development replacements and are excluded from publication.

## Review results

| Pass | Result |
| --- | --- |
| API and behavior | Portable guarantees, ownership, cancellation, errors, and driver boundaries reviewed. Oversized individual and aggregate schema pointers are now rejected before copying or JSON encoding, with zero-allocation regression checks. The shared contract now also proves all-term matching across text fields and explicit field selection, including a document with an absent optional field. |
| Drivers and integration | All eight drivers pass all 38 shared scenarios. The complete run passes 421 tests including subtests, with real embedded engines and authenticated Testcontainers services. |
| Quality and security | 945 unit tests including subtests pass. Linux unit statement coverage is exactly 100%. Race tests, vet, tidy parity, and a 15-second document-validation fuzz run pass. Root and Bleve vulnerability scans find no vulnerabilities using govulncheck v1.9.0 on Go 1.27.3. |
| Packaging and documentation | The release plan validates both tag names and aligned requirements. A fresh consumer passes against staged root and Bleve module archives with `GOWORK=off`, no replacements, and an isolated module cache. Documentation regeneration is stable across two runs. |

The staged consumer exercises embedded provisioning, writes, visibility, and search; constructs the HTTP service clients; and checks SQL driver composition. Real service behavior is verified separately by the integration suite. Staged archive verification proves candidate composition, not public availability through the Go module proxy.

Executed test inventories, backend versions, scenario lists, and source identity are recorded in [unit results](unit-results.json), [integration results](integration-results.json), [driver evidence](driver-results.json), and [coverage](coverage.json). CI and Security must pass on the final candidate commit before tagging.

## Publication handoff

Follow the [release order](development.md#release-order) after approving publication. Update the unreleased README notice and status badge for the actual release, publish the root tag before the Bleve tag, and verify each module resolves independently. Then run:

```sh
make release-check v0.1.0
```

This final check must use the public module versions without the staged proxy or local replacements. A successful staged consumer is not a substitute for verifying the published modules.
