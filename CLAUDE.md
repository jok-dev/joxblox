Tip: check ./latest.log for latest logs of last run

# Code Quality Overview

## Simplicity
- Keep solutions as simple as possible while meeting requirements. (unless larger changes would reduce duplication, make the code easier to read, etc)
- Prefer small, incremental changes over large rewrites. (unless larger rewrites would reduce duplication, make the code easier to read, etc)
- Introduce abstraction only when it reduces clear duplication or complexity.

## Readability
- Use clear, consistent naming and straightforward control flow.
- Keep files and functions focused on a single responsibility.
- Reduce repeated literals and repeated logic through small shared helpers.

## Structure
- Separate presentation concerns from business/data logic where practical.
- Favor reusable components/helpers for shared behavior.
- Avoid over-engineering; choose maintainability over novelty.

## Security
- Never store sensitive data in plaintext.
- Validate external inputs and fail safely.
- Keep security-related behavior explicit and easy to audit.

## Quality Checks
- Format code consistently before finishing changes.
- Verify build/test passes after substantive edits.
- Preserve existing behavior unless a change is intentional and documented.
- Don't do stuff on the ui thread if it's gonna take a while
- Always use the same logic for showing byte sizes, show mb if >1mb, kb if >1kb and if not then show bytes
- Make sure that any feature that uses assets also supports rbxthumb:// notation since this returns different asset sizes than just using the asset id itself! don't strip this from ids it's important!
- Make sure to use the same shared methods as other systems in the app so that each feature uses the same backend and therefore has the same consistent behavior and features

## Private Test Fixtures - NEVER Leak

- Real Roblox `.rbxl` / `.rbxm` / `.obj` test fixtures live OUTSIDE this repo at `../joxblox-private-tests/`. They are proprietary game content and MUST NEVER enter the repo, git history, a PR, a commit message, chat context you paste elsewhere, or any external service.
- Do not copy, move, symlink, or `cp` these files into the working tree. Do not `cat` their contents into a response. Do not commit a test that hard-codes their contents.
- The extractor's private test suite ([internal/extractor/private_triangle_test.go](internal/extractor/private_triangle_test.go)) auto-skips when `../joxblox-private-tests/` is absent - that is intentional; do not "fix" this by bundling fixtures. Each subdirectory of that folder is one pair and must contain exactly one `.rbxl`/`.rbxm` and exactly one `.obj` (discovered by extension, no manifest).
- `.gitignore` is configured to block `*.rbxl`, `*.rbxm`, `*.obj`, and `joxblox-private-tests/` as a second layer of defense. If you find yourself about to add an exception to that ignore list, stop and ask first.
- If a test failure diagnostic needs fixture content, summarize statistics (counts, sizes) - never paste raw bytes or asset IDs from the file.
