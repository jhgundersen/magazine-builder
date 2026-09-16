# Initial generation-pipeline review

## Fixed in this change

- Multi-page prompts used `articleSection.ImageBrief` as `brief_body`, omitting the rewritten section copy. Copy and illustration instructions now occupy separate fields; regression tests check each page.
- `imagePromptJSON` cut serialized JSON at 3,900 characters before furniture injection. The render limiter could cut it again. Planning now preserves JSON; rendering trims the existing optional style fields and rejects an insufficient budget with an actionable error. This can expose explicit errors where the previous code silently sent broken prompts.
- Furniture instructions allowed issue metadata in footers despite the project contract. Both headers and footers now explicitly exclude it.

## Next improvements, in priority order

1. **Prompt budget allocation:** the default render budget is 3,990 characters. Allocate this deliberately across page copy, image directions, style, identity and furniture. Prefer trimming optional modules and redundant overview before reducing copy. Measure representative Norwegian comics, long features and poster prompts. Current code preserves essential content and reports over-budget requests; it does not yet optimize all fields to fit.
2. **Issue capacity:** `planMagazine` ends at the requested page count even when an article still has remaining parts. Preflight required article pages plus cover/back/adverts; report overflow or propose a larger issue before rewriting and rendering.
3. **Style-derived generation lengths:** `generateArticles` asks for 900–1,500 characters and `rewriteFeatureForStyle` for 700–1,400 regardless of `articleLength`. Reuse the existing length guidance helpers, preserving comic/puzzle/listing formats. Review multi-page guidance too: page allocation and the stated per-page range should agree.
4. **Source fidelity across multi-page rewrites:** single-page imported rewrites explicitly preserve facts, names and chronology; the multi-page prompt should carry the same contract. Validate the returned section count before marking the article enhanced.
5. **Fallback furniture:** the fallback uses the article title as the section slug and generic “Page”/“Side” footers. Use localized department labels and the publication name without inventing issue metadata.

## Visual evaluation still needed

Compare a short comic, a Norwegian magazine, a long multi-page feature and an interior poster. Inspect copy fidelity, continuity, typography, palette, furniture and valid final page references. Unit tests verify data flow and prompt integrity; no paid image generations or visual comparisons were performed in this pass.
