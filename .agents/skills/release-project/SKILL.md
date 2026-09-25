---
name: release-project
description: Prepare a Camunda Stub Worker minor or patch release by checking README and site documentation, refreshing screenshots, presenting changes for review, merging a documentation PR after CI passes, and running the release GitHub Action.
---

# Release Camunda Stub Worker

Use this workflow when asked to release this project. Creating or editing this skill does not itself request a release. Honor a request limited to preparation or documentation review.

## 0. Pull latest main and establish the release

- First, synchronize the release source with `origin/main` before auditing documentation or choosing a version. Read applicable repository instructions and inspect the working tree, then switch to `main` and run `git pull --ff-only origin main`. Preserve unrelated local work; if switching or pulling would disturb it, fetch `origin/main` and use an isolated worktree based on that ref instead. Do not reset, discard, or automatically stash the user's changes. If local `main` has diverged or contains unpublished commits, use the remote-based worktree rather than including those commits in the release.
- Read applicable repository instructions, inspect the working tree, and confirm the GitHub repository and default branch from the remote. The current repository is `mishankov/camunda-stub-worker`, with default branch `main`; verify these rather than assuming they never change. Use authenticated `gh` or an available GitHub connector.
- Preserve unrelated local work. Prepare a `codex/release-docs-<version>` branch from the current remote default branch, using an isolated worktree when needed.
- Take `minor` or `patch` from the user's request. If missing, ask which while continuing the documentation audit; do not infer it from the size of the diff.
- Fetch tags and inspect published, non-draft, non-prerelease GitHub releases. Use the highest stable semantic version as the baseline, not `frontend/package.json` (its private package version is not the app release version). Patch maps `vX.Y.Z` to `vX.Y.(Z+1)`; minor maps it to `vX.(Y+1).0`. If no stable baseline exists, ask for the initial version.
- Read `.github/workflows/release.yml`, `build.yml`, `pages.yml`, and `.github/release-notes.md` on each invocation. The release workflow currently accepts `tag` and `prerelease`, injects the tag into `main.version`, and creates/publishes the release itself. Do not add an unrelated package-version bump or pre-create a release.

## 1. Audit and update documentation

Compare the current source and behavior on the release branch with `README.md` and `site/index.html`, including changes since the baseline release. Check feature claims, UI labels, setup steps, defaults, supported Camunda versions, limitations, local data and credentials, downloads, installation requirements, and development commands against source, dependency files, and workflows. Correct stale text in both places where relevant. Keep verification claims tied to actual evidence.

The documentation site is the plain HTML/CSS site in `site/`, deployed by GitHub Pages. Its expected URL is `https://mishankov.github.io/camunda-stub-worker/`. Inspect the live site as well as local sources, accounting for changes that have not been deployed yet.

Inspect the actual screenshot images and compare them with the current app. Images currently come in matching pairs:

- `docs/images/job-types.jpg` and `site/assets/job-types.jpg`
- `docs/images/manual-response.jpg` and `site/assets/manual-response.jpg`
- `docs/images/start-process.jpg` and `site/assets/start-process.jpg`
- `docs/images/connections.jpg` and `site/assets/connections.jpg`

Refresh only stale or misleading screenshots. Capture the current app with illustrative sample data and masked credentials, using a disposable profile/database. Do not expose real tokens, passwords, or private process data. Use real UI captures, not generated imitations. Copy each refreshed image to both locations and check that the copies match. Update captions, alt text, and references if filenames or content change. If capture is unavailable, explain what remains unverified rather than claiming completion.

Preview with `python3 -m http.server 4173 --directory site` from the repository root (choose another port if occupied). Inspect the rendered site at desktop and narrow widths, Markdown image references, navigation, relative asset URLs, and download links. The site needs no build step. Run `git diff --check`. For app changes or app-backed capture, use the relevant checks from CI: `npm ci`, `npm run check`, and `npm run build` in `frontend/`, and `go test -race ./...` at the root. Report checks actually run and any limitations.

## 2. Present changes for user review

Before pushing or creating the PR, show the proposed version, a concise summary of corrections, clickable file/diff links, site preview, refreshed screenshots, and validation results. Ask the user to approve the documentation changes; explicitly explain that this pause implements their requested review-before-PR step. Wait for their response and incorporate requested revisions.

Approval of these changes in a full release request authorizes the remaining PR, CI, merge, and release steps. Do not ask for the same approval again. If material new documentation changes become necessary later, show those changes for review before proceeding. If the audit finds no changes, present that finding for review and explain that the empty documentation PR will be skipped; do not manufacture edits.

## 3. Create the documentation PR

After approval, commit only the reviewed documentation and screenshot files, push the release-docs branch, and create a PR against the default branch. Include the concrete documentation corrections, screenshot changes, and validation evidence. Use `gh pr create --body-file` for a multiline description. Keep unrelated working-tree files out of the commit. Reuse an existing PR for this release on resumption instead of creating a duplicate. Share its link.

## 4. Babysit CI and merge

Keep following the PR until it is merged or a specific blocker needs user action. Use bounded CI polling so progress can still be communicated. Inspect failed job logs and fix failures caused by this PR within scope. Rerun a clearly transient failure once; do not loop on unchanged failures or modify product behavior just to obtain a green check.

Before merging, verify that all required checks and the complete `build.yml` run succeeded for the latest PR head. Currently that means the test job and desktop builds for Windows x64, macOS x64, macOS arm64, and Linux x64. Pending, cancelled, missing, and failed checks are not success. Recheck after every new commit or conflict resolution. Honor required reviews and branch protection; never bypass them with an admin merge.

Merge using a repository-supported method and pin the expected PR head with `gh pr merge --match-head-commit <sha>` where available. Verify the PR is actually merged, recording its merge commit. If merge queues are required, follow the queue to completion. An enabled auto-merge setting is not a completed merge.

Wait for the default-branch build covering the merged source. If site files changed, also verify the Pages deployment and live site update. If a deployment or CI failure persists, report its link and blocker before releasing. If no documentation PR was needed, apply the same CI verification to the default-branch source selected for release.

## 5. Dispatch and verify the release

Re-read the remote release state immediately before dispatch. Confirm the chosen version is still the next minor/patch version and that its tag and release do not already exist. If another release advanced the baseline, recompute the version using the requested bump and announce the new value. Inspect any existing tag, draft, release, or in-progress run for the intended version before acting; resume a known attempt rather than overwriting an unrelated release.

Confirm the default branch includes the documentation merge and that its current head has passing CI. The current workflow creates a new tag from the repository's default branch, even when a different dispatch ref is used. Check for branch movement, assess newly included changes for documentation accuracy, and wait for their CI before dispatching. Do not claim that specifying `--ref` pins the release source.

With verified values substituted, run:

```bash
gh workflow run release.yml --repo <owner/repo> --ref <default-branch> -f tag=<vX.Y.Z> -f prerelease=false
```

Identify the newly dispatched run by workflow, event, branch, creation time, and requested tag (inspect the prepare job/release when needed). Do not blindly watch the latest run if another release is in progress. On an ambiguous dispatch response, inspect runs and release state before retrying.

Wait for the release action to reach a terminal status; dispatch alone does not complete the task. Watch through source tests, all four builds, asset upload, and publication, providing concise progress updates while it runs. Report its final conclusion explicitly, including failure or cancellation. Verify the created tag resolves to the intended tested source containing the documentation merge. If the source differs unexpectedly, stop further release actions and report it. On failure, inspect logs and existing draft/tag/assets; retry only after understanding the cause and verifying the same release target. Do not delete or retarget a published tag or repeatedly redispatch a workflow that uses asset overwrite.

Success requires a successful workflow and a published, non-draft stable release with the expected tag, Windows ZIP, both macOS DMGs, Linux tarball, and `checksums.txt`. Report the released version, documentation PR (or no-change finding), workflow link, and release download link. If blocked, report the exact remaining step and recovery information instead of announcing a completed release.
