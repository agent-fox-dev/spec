Review the plan below, generated against the code in {{root}} from the PRD that follows. This is round {{round}} of at most {{limit}}. The plan answers to the PRD; you are not shown the input the PRD was written from.
{{prd_block}}
## The plan under review

--- BEGIN PLAN ---
{{plan}}
--- END PLAN ---
{{facts_block}}{{notes_block}}{{earlier_block}}
Your file tools take paths relative to {{root}}; a path outside it is refused, and retrying will not change that.

Check the plan against the PRD, the grounding facts and the code, and call submit_plan_review with every flaw you found.
