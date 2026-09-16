# Initial generation-pipeline review

## Fixed in this change

- Multi-page prompts used `articleSection.ImageBrief` as `brief_body`, omitting the rewritten section copy. Copy and illustration instructions now occupy separate fields; regression tests check each page.
- `imagePromptJSON` cut serialized JSON at 3,900 characters before furniture injection. The render limiter could cut it again. Planning now preserves JSON; rendering trims the existing optional style fields and rejects an insufficient budget with an actionable error. This can expose explicit errors where the previous code silently sent broken prompts.
- Furniture instructions allowed issue metadata in footers despite the project contract. Both headers and footers now explicitly exclude it.

## Follow-up improvements implemented

- Style enhancement now receives the full original user request alongside the inferred brief, with explicit responsibilities for each guide field, concrete hierarchy and layout decisions, palette usage, and cross-page consistency. The publication name is enforced after generation.
- Text prompts receive structured style JSON instead of a prose summary truncated at 900 characters, preserving trailing exclusions and page rules.
- Standard articles use `content`, short articles use `short`, and covers/features/adverts/fillers/back pages receive their respective layout notes. Shared `core` stays independent of page layout. Posters and brand boards no longer inherit feature or cover composition.
- All page style blocks include `color_usage` alongside the hex palette; palette normalization repairs individual missing/invalid roles while preserving valid supplied colors.
- Creative-kit pools receive their matching department/advert/sidebar/back-page style rules. Article layout instructions defer to the guide's panels, lists, puzzles or other requested structure.
- Image budgeting removes optional article modules and repeated story overview before shortening style prose. Essential copy, image briefs, identity, palette and furniture remain intact. Representative comic and magazine cover/feature/poster/filler fixtures fit the default 3,990-character budget, including furniture where applicable.
- Generated articles, feature briefs and imported rewrites use the existing style-derived length helpers. Multi-page rewrites use a per-page range rather than the total article range and explicitly preserve source facts and recurring visual descriptions.
- Multi-page responses must contain the requested number of nonempty copy/image-brief pairs before being marked enhanced. Failures preserve the original article.
- Furniture fallbacks use department labels and the publication name instead of a long article title and generic “Page” footer.

## Remaining work

- **Issue capacity:** `planMagazine` can still run out of issue pages before an article finishes. Preflight capacity and present an appropriate larger issue size before generation.
- **Long-copy budgeting:** essential content beyond the configured limit still produces an explicit error; further copy fitting should be intentional and preserve facts rather than silently truncating text.
- **Length policy:** the existing helper still caps per-page copy at 1,600 characters. Revisit that cap together with rendered typography and image prompt budgets rather than changing it independently.

## Visual evaluation still needed

Compare a short comic, a Norwegian magazine, a long multi-page feature and an interior poster. Inspect copy fidelity, continuity, typography, palette, furniture and valid final page references. Unit tests verify data flow and prompt integrity; no paid image generations or visual comparisons were performed in this pass.
