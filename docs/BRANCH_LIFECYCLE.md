# Branch lifecycle and merged-PR pruning

This repository uses `master` as the canonical baseline. Closing a PR is
**not** the same as merging it. Do not delete a branch merely because its code
appears elsewhere, has a similar name, or is old.

## Deletion predicate

The dedicated workflow `.github/workflows/prune-merged-pr-branches.yml`
selects a remote branch only when **all** conditions hold:

1. It is not the current default branch, a GitHub-protected branch, or a
   `release/`, `hotfix/`, `backup/`, `archive/` or `keep/` branch.
2. There is no open PR using the branch as a same-repository source.
3. A same-repository PR targeting `master` was actually merged, **and**
   the current branch head SHA exactly matches that merged PR's head SHA.
4. At deletion time Git's `--force-with-lease=refs/heads/<name>:<SHA>`
   still matches the remote SHA. Concurrently advanced branches are retained.

Only the remote branch ref is deleted. Merged commits, PR discussion, CI
evidence, tags and local checkout branches are not rewritten or removed.
Unmerged/closed-without-merge PRs and branches without exact evidence require
manual review, not automatic pruning.

## Execution and review

- The first landing of this workflow onto `master` triggers **one initial
  apply run** (push event filtered to the workflow file itself).
- Further cleanup is manually available in GitHub Actions via
  **Prune safely merged PR branches → Run workflow**; the default is a
  **dry run**. Select `apply=true` only after reading the candidate list.
- The workflow uses repository-scoped `GITHUB_TOKEN` with `contents:write`
  solely for SHA-fenced branch ref deletion, and `pull-requests:read` for
  confirmation. If permissions or repo rules reject deletion, the workflow
  reports a failure rather than bypassing protections.
- A GitHub Actions run summary records candidate/deleted/failure counts.
  Cleanup is limited to 80 candidates per run to avoid unexpectedly broad
  changes.

## 2026-10-15 audit baseline

- 81 remote branches, including `master`.
- 56 branches with exact merged-PR head evidence.
- 9 open PR branches and `master` must remain.
- 15 unmatched branches require separate evidence-based review (some appear
  to be aliases or older discarded prototypes).

This is a **pre-cleanup inventory**, not a claim that remote branch refs have
already been deleted. Audit counts must be recomputed by the action at run time.

## Other governance gates

Enable GitHub repository setting **Automatically delete head branches**
for future merged PRs, if the repository owner accepts that policy; it is
currently off. Still configure `master` branch protection/rulesets and
required CI checks (#38). Automatic merged-branch deletion does not substitute
for required reviews, signed artifacts, protected deployments or #79's
production identity controls.
