Revise the plan below, generated against the code in {{root}} from the PRD that follows, to answer round {{round}} of at most {{limit}} of its review.
{{prd_block}}
## The plan under revision

--- BEGIN PLAN ---
{{plan}}
--- END PLAN ---
{{findings_block}}{{facts_block}}{{notes_block}}{{earlier_block}}
Your file tools take paths relative to {{root}}; a path outside it is refused, and retrying will not change that.

Answer every finding, and call submit_plan_revision with the three revised artifacts and your dispositions.
