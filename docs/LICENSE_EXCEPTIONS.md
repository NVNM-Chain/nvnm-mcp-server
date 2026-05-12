# License exceptions

Per the proprietary commercial licensing policy in `CLAUDE.md`, the
default disposition for GPL-3.0 / LGPL-3.0 / AGPL-3.0 dependencies is:

- **GPL-2.0, AGPL-3.0**: hard refusal. Not on the CI allowlist.
- **GPL-3.0, LGPL-3.0**: case-by-case. **Require explicit approval**
  before adding; document the rationale in this file.

## Active exceptions

### `github.com/ethereum/go-ethereum`

| Field | Value |
|---|---|
| go-licenses classification | `GPL-3.0` |
| Actual library license | LGPL-3.0 for consumed packages (`common/`, `core/types/`, `accounts/abi/`, `ethclient/`, ...); GPL-3.0 applies only to the `cmd/` binaries the project does not consume. The library directories ship `COPYING.LESSER` overriding the repo-root `COPYING` (GPL-3.0). `go-licenses` does not honor the per-directory override and surfaces the repo-root classification. |
| Linkage | Static. `CGO_ENABLED=0` Go binary. LGPL-3.0 § 4 requires "Combined Works" to either (a) permit re-linking against modified versions of the library, or (b) ship under a license that itself permits the same. |
| Approval status | **Pending business decision.** Treat as provisional. |
| Approver | TBD |
| Approval date | TBD |
| Rationale | The project is built on go-ethereum's RPC client, transaction types, ABI encoder, and `common.Address`/`common.Hash` types from day one. Removal is a significant refactor (see "Replacement alternatives" below). |
| Mitigation under LGPL-3.0 § 4 | Two viable paths: (a) ship the object files / build artifacts so a recipient can re-link against a modified go-ethereum; (b) re-license the project under a compatible license. Neither is currently in place. |
| Action items | (1) Confirm the actual transitive license of every consumed package via per-package inspection, not go-licenses' repo-root classification. (2) Evaluate replacement (see below). (3) If replacement is rejected, formalize the LGPL-3.0 obligation in the release pipeline (publish object files or compiled binaries with re-link instructions). |

#### Replacement alternatives evaluated

(populated by research task; see separate analysis)

## Process

Adding a new exception:

1. Open a PR that adds the dependency and updates this file.
2. The exception requires explicit human approval in the PR. Do not
   merge until the Approver and Approval date fields are filled.
3. The CI license allowlist may temporarily admit the license while
   the PR is in review; in steady state, this file should describe
   every license outside the unconditional allowlist.
