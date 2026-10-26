# DCGM weighted GPU utilization compatibility

OpenAgent derives `DCGM_FI_DEV_WEIGHTED_GPU_UTIL` from NVIDIA dcgm-exporter
samples, before metric relabeling and target/node/project labels are applied.
The original metrics are not rewritten. No exporter, Operator, UI, alarm,
sender protocol, or recovery policy is changed by this implementation.

## Compatibility contract

The reference is WhaTap dcgm-exporter commit
`cfba89f78564485f84cdf136868b84959eb0b0ad`,
`internal/pkg/transformation/weighted_util.go`, with renderer labels from
`internal/pkg/rendermetrics/render_metrics.go`. That commit descends from NVIDIA
release tag `4.6.0-4.8.3`; its diff also contains profiling self-heal changes.
The legacy `test/integration/weighted.go` program is not the production contract
and is not used or executed by these tests.

- Name: `DCGM_FI_DEV_WEIGHTED_GPU_UTIL`; type: `gauge`;
  HELP: `Weighted GPU Utilization`; unit: ratio in `[0, 1]`.
- Whole GPU: `DCGM_FI_DEV_GPU_UTIL / 100`, `calculation_method="direct"`.
- Whole H100/H200/H800/H20: if both GPU_UTIL identity and non-MIG
  GR_ENGINE_ACTIVE are present, use the latter ratio directly with
  `calculation_method="prof_gr_engine_active"`. This is the fork's model branch,
  not a generic preference for profiling on every GPU. If profiling is absent,
  use GPU_UTIL; if the preferred observation is invalid, do not manufacture a
  fallback value. Profiling alone does not identify the fork's whole-GPU branch.
- MIG: `sum(GR_ENGINE_ACTIVE * profile_slices) / MIG_MAX_SLICES`,
  `calculation_method="weighted_sum"`. Summing the numerator first avoids
  accumulated division-rounding at full load. Parity is numerical, not identical
  decimal serialization of floating-point intermediate results.
- Non-MIG output retains the selected source observation's labels, including
  namespace/pod/container/UID and custom workload attributes, then applies the
  compatibility `DCGM_FI_DEV_UUID` and `calculation_method` labels. Ordinary
  GPUs use GPU_UTIL attribution; H-series uses the selected GR_ENGINE_ACTIVE
  attribution (or GPU_UTIL attribution on fallback). Equal-valued, equal-time
  observations of the same GPU retain distinct workload series; identical
  output rows are deduplicated. Different workload values/timestamps remain
  conflicting evidence: they are neither summed nor treated as reconstructed
  physical utilization. The reference runs WeightedUtil before PodMapper and
  renders the resulting workload attributes on whole-GPU weighted output.
- MIG physical-parent output preserves physical GPU identity (`gpu`, configured `UUID` or
  `DCGM_FI_DEV_UUID`, PCI bus, device, model), hostname/node/source scope, and
  known device-wide metadata labels (`DCGM_FI_DRIVER_VERSION`,
  `DCGM_FI_DEV_MIG_MODE`, `DCGM_FI_DEV_MIG_MAX_SLICES`, `DCGM_FI_DEV_UUID`).
  Add `DCGM_FI_DEV_UUID` as the fork does. GI/CI, pod, namespace, container, UID,
  and custom/per-GI metadata (even if its name starts with `DCGM_FI_`)
  are not promoted to the MIG physical parent. No new label is inferred from a GPU
  model. Source-level target labels are still handled by the existing processor.
- Existing weighted samples are producer-owned and suppress derivation for
  that scoped GPU only, including unavailable producer values. Existing HELP
  remains unchanged. Other GPUs in the same scrape can still be derived.

There is no target-name-only heuristic: derivation requires actual DCGM field
names, a numeric `gpu`, a physical `GPU-` UUID, and consistent model/device
identity. Grouping is local to one decoded scrape and includes GPU index, UUID,
host/node and available source labels (`job`, `instance`, `cluster`,
`exported_job`, `exported_instance`, `endpoint`). There is no global UUID cache
or join between targets/scrapes. Independent exporter source scopes remain
independent. This is not a general federated/recording-rule aggregation engine.

## Required exporter inputs

The Operator CSV inspected in `internal/gpu/csv.go` declares MIG metadata as
**labels**, not gauges. A compatible official-exporter CSV includes:

```csv
DCGM_FI_DEV_MIG_MODE, label, MIG mode status.
DCGM_FI_DEV_MIG_MAX_SLICES, label, Maximum MIG slices available.
DCGM_FI_DEV_GPU_UTIL, gauge, GPU utilization (in %).
DCGM_FI_PROF_GR_ENGINE_ACTIVE, gauge, Ratio of time the graphics engine is active.
```

MIG_MODE and MIG_MAX_SLICES may alternatively be gauges, provided they match the
same physical/source identity and observation timestamp. Do **not** include
WEIGHTED_GPU_UTIL in the official NVIDIA CSV: it is a fork-owned field. The
upstream default counter CSV inspected at that tag does not supply these MIG
metadata fields; the default CSV alone is not sufficient for MIG derivation.

Flex-shaped GI samples are supported without requiring `-d g+i`. When both
parent and GI rows are supplied, derive from GI rows only; never add a physical
parent GR_ENGINE_ACTIVE/GPU_UTIL observation into the MIG sum. Duplicate
namespace/pod attribution rows for the same GI count once only if value,
profile, timestamp and metadata agree. Conflicts suppress the derived GPU row.

## Unavailable is not zero

No derived row is emitted for missing capacity, malformed/unsupported profile
(`Ng.Mgb` and `Ng.Mgb+me` are recognized), missing known-GI activity, conflicting
identity/mode/capacity, oversubscribed slice totals, NaN/Inf, DCGM sentinels,
negative values or out-of-range utilization. Capacity is supplied by the
exporter, never assumed to be seven or guessed from model names. Actual measured
zero is retained. A non-MIG capacity of zero is allowed because it is not used
as a denominator.

Unlike the fork, this implementation deliberately does not copy its
missing-capacity fallback to seven, permissive profile parsing, or clamp invalid
GPU_UTIL values to 100. The DCGM subset is strictly parsed for derivation while
legacy source parsing remains unchanged. Malformed text, invalid DCGM input
types or duplicate label keys disable derivation for that scrape; source data
still follows the existing conversion path. Missing protobuf values are not
interpreted as measured zero by the derivation.

Only GIs represented somewhere in the scrape can be checked for completeness.
If an exporter omits an entire GI (all its metrics), OpenAgent cannot distinguish
that from genuinely unallocated capacity. Identically attributed copies can be
deduplicated; conflicting/per-process attribution cannot reconstruct missing
physical observations. Arbitrary custom exporter UUID-label names other than
`UUID`/`DCGM_FI_DEV_UUID` are not inferred.

## Offline verification and release boundary

`pkg/converter/dcgm_weighted_test.go` contains **synthetic** NVIDIA-shaped
fixtures, not hardware captures. `pkg/processor/dcgm_weighted_test.go` uses real
loopback HTTP scraping in text and delimited-protobuf formats, then the real
converter, metric relabeling, target/node/pcode enrichment, sender input queue,
and OpenMxPack serialization/deserialization. No secure sender is started.

Run with Go 1.24.3 (the module toolchain) and an isolated, empty `WHATAP_HOME`
set **before** package initialization. Configuration initialization creates or
reads `whatap.conf`; do not run tests with a production configuration directory.
Use a scratch HOME, TMPDIR and an allowlisted environment without credentials.

```sh
go test ./pkg/converter ./pkg/processor ./pkg/scraper ./pkg/client ./pkg/model ./pkg/config -count=1 -timeout=90s
go test -race ./pkg/converter ./pkg/processor ./pkg/scraper -count=1 -timeout=90s
go build -o <scratch>/openagent .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o <scratch>/openagent-linux-amd64 .
```

The pre-existing `pkg/sender/duplicate_check_test.go:15` calls `NewSender` with
two arguments although the constructor requires three; this independently
blocks `go test ./pkg/...` on the inspected base commit. It is not fixed here.

No NVIDIA exporter process, GPU hardware, live cluster, collector/backend, or
UI was exercised by these offline tests. Live side-by-side numeric/label parity
and driver/MIG soak remain required before switching deployment images.
Roll out the compatible OpenAgent first and verify it is active on all affected
scrape paths; only then switch the Operator's exporter image and CSV. Publishing
both releases together does not guarantee this rollout order.
`version.properties` remains `VERSION=1.8.12`; a new release
version is required before publication. No commit, push, PR, image publication,
Operator configuration change or deployment is part of this patch.
