// Package evalgen builds synthetic evaluation items from the committed seed
// corpus (Norwegian statutes and regulations).
//
// It is the generator half of the evaluation pipeline, distinct from the
// exam-archive extractor in internal/evaldata. It emits three item families:
//
//   - synthetic: Q&A/MCQ items whose question is templated from an act's
//     short_title and a provision's section_label, with the provision's own
//     text as gold points and a lovcite-validated reference as gold refs.
//     A configurable fraction of these are deliberately unanswerable
//     (fabricated section numbers or non-existent act keys) so the harness can
//     measure refusal behaviour.
//   - retrieval: corpus-derived items asking "which provision in <act>
//     regulates <topic>?", carrying a task:"retrieval" marker and a doc_ref
//     pointing at the ground-truth paragraph key.
//   - nordercase: a thin adapter that downloads the Nor-CaseHOLD test split and
//     maps each case-law row to a retrieval-shaped item flagged out_of_graph.
//
// Every gold_ref is gated through internal/lovcite against the seed; a
// paragraph whose reference does not resolve is skipped, never emitted.
package evalgen
