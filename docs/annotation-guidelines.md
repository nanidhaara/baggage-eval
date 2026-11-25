# Annotation Guidelines

These rules make every evaluation decision consistent and reproducible.
The automatic rules live in `internal/evaluation/rules.go`; human reviewers
follow the same order when they give feedback.

## Labels

| Label | Meaning |
|---|---|
| `CORRECT` | Predicted category matches the ground truth and confidence ≥ review threshold (0.75) |
| `INCORRECT` | Predicted category differs from the ground truth |
| `INCOMPLETE` | A required attribute (`color`, `size`) is missing or empty |
| `LOW_CONFIDENCE` | Confidence is below the minimum threshold (0.50) |
| `REVIEW` | A human must decide: there is no ground truth, or the category matches but confidence is between 0.50 and 0.75 |

## Decision order (first match wins)

1. **Required attributes present?** If not → `INCOMPLETE` (`MISSING_ATTRIBUTES`)
2. **Confidence < 0.50?** → `LOW_CONFIDENCE` (`LOW_CONFIDENCE`)
3. **Ground truth available?** If not → `REVIEW` (`NO_GROUND_TRUTH`)
4. **Category matches?**
   - confidence ≥ 0.75 → `CORRECT`
   - confidence 0.50–0.75 → `REVIEW` (`BORDERLINE_CONFIDENCE`)
5. **Category does not match** → `INCORRECT`
   - confidence ≥ 0.90 → error category `HIGH_CONFIDENCE_MISCLASSIFICATION` (most serious: the model is sure and wrong)
   - otherwise → `WRONG_CATEGORY`

Boundaries: a value exactly equal to a threshold passes it (0.75 counts as confident, 0.50 is not low confidence).

## Ground truth

- Default ground truth is `expected_category` on the baggage record.
- If a reviewer submits feedback with `correct_category`, that value becomes the ground truth for that bag and the evaluation is re-run automatically.
- The latest reviewer correction wins.

## Category definitions

| Category | Use when |
|---|---|
| `SUITCASE` | Rigid or semi-rigid case with handle, usually wheeled |
| `HARD_CASE` | Protective shell case (instrument, equipment, aluminium cases) |
| `BACKPACK` | Carried on the back with two straps |
| `DUFFEL_BAG` | Soft cylindrical bag, top opening, no frame |
| `STROLLER` | Baby stroller / pram |
| `GOLF_BAG` | Golf club bag |
| `SPORTS_EQUIPMENT` | Skis, bikes, surfboards, cricket kits and similar |
| `OTHER` | Anything not covered above (boxes, cartons, odd-size items) |

## Writing reviewer comments

- Say *what you saw* and *why* you chose the label: "Image shows aluminium shell case, not a suitcase."
- If the record itself is wrong, set `correct_category` and mention it in the comment.
- Do not guess when the image is unclear — use `REVIEW` and explain what is unclear.

## Error categories used in reports

`MISSING_ATTRIBUTES`, `LOW_CONFIDENCE`, `NO_GROUND_TRUTH`, `BORDERLINE_CONFIDENCE`,
`WRONG_CATEGORY`, `HIGH_CONFIDENCE_MISCLASSIFICATION`.
Input problems (rejected before evaluation) are recorded in `processing_errors`
as `VALIDATION`, `DUPLICATE`, `NOT_FOUND` or `INTERNAL`.
