---
name: session-review
description: Review the current session's progress, main uncertainty, and possible blind spot against its intended goal. Use when explicitly invoked for a candid session checkpoint.
---

# Session review

Review the current session using the conversation and relevant available evidence. Keep the focus on the user's intended outcome, including accepted changes in direction. Do not treat the latest completed task as proof that the overall goal has been achieved.

Use narrow read-only checks when they would materially improve the assessment and existing permissions allow them. Identify the basis of the review and any important missing context. Treat earlier assistant conclusions as claims to assess, not independent evidence.

Address these questions:

1. Where are we now? Compare the intended goal with what is completed, what is verified, and what remains unresolved.
2. What am I least confident about? Choose the most consequential uncertainty. Explain the evidence gap, why it matters, and what would increase or reduce confidence. Distinguish missing context from a demonstrated project problem.
3. What might we be overlooking? Identify one material assumption, tradeoff, or framing issue that deserves attention. Consider whether the current work serves the intended goal. Present a possible blind spot as an inference, not a claim about what the user knows.
4. What should we check next? Recommend the smallest useful check or decision that addresses the uncertainty or possible blind spot. State what its result would help decide.

Be candid and specific. Separate observed facts, inferences, and unknowns. Do not invent criticism to fill a section, assign unsupported numerical confidence, or defend earlier recommendations merely because you made them. If the evidence does not support a meaningful blind spot, say so. Do not mistake passing checks for proof of outcomes they do not measure.

Keep the response short by default: a status paragraph followed by the main uncertainty, one possible blind spot, and a next step. Expand only when the user asks or the situation needs it. Avoid a generic risk inventory or a recap of every action in the session.

Review and recommend; do not edit files, execute project code, launch remediation, or create new tasks solely because the review identifies a next step. Continue to respect the repository's existing permissions and working agreements.
