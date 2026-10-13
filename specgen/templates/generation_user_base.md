Generate the {{artifact}} artifact for spec "{{spec_id}}" ({{spec_name}}), against the code in {{root}}.

PRD:

{{prd}}
{{notes_block}}{{landscape_block}}{{relevant_files_block}}{{prior_block}}
Every ID you reference must come from the PRD or from the artifacts above. Your submission is validated against the format's schema and every cross-file rule decidable at this point, before it is written; a violation comes back to you naming the rule that failed. Correct it with patch_{{artifact}}, which edits just the fields that fail in the submission it rejected, rather than writing the whole artifact again. A submission that passes may come back once with the validator's warnings on its wording; fix what is worth fixing.
