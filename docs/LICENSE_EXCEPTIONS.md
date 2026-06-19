# License exceptions

Per this project's dependency-license policy, the
default disposition for GPL-3.0 / LGPL-3.0 / AGPL-3.0 dependencies is:

- **GPL-2.0, AGPL-3.0**: hard refusal. Not on the CI allowlist.
- **GPL-3.0, LGPL-3.0**: case-by-case. **Require explicit approval**
  before adding; document the rationale in this file.

## Active exceptions

**None at this time.**

The previous entry for `github.com/ethereum/go-ethereum` (LGPL-3.0
under static linking) was resolved on 2026-05-13 by replacing it with
`github.com/defiweb/go-eth` (MIT). The migration is recorded in
`docs/SECURITY_AUDIT.md`. The CI license allowlist no longer carries
GPL-3.0 or LGPL-3.0; any future dependency under those licenses
requires an explicit approval entry here before the allowlist can be
relaxed.

## Process

Adding a new exception:

1. Open a PR that adds the dependency and updates this file.
2. The exception requires explicit human approval in the PR. Do not
   merge until the Approver and Approval date fields are filled.
3. The CI license allowlist may temporarily admit the license while
   the PR is in review; in steady state, this file should describe
   every license outside the unconditional allowlist.
