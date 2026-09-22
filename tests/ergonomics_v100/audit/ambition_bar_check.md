# Ambition check, pass 1

The skill's self-prompt was applied after the independent post-pass scores:

> That's it?? I was hoping you would get a lot more practical value out of this skill.
> Where are the dramatic improvements? Re-read the playbook, look at the surfaces still
> scoring below 500 on output_parseability / error_pedagogy / intent_inference /
> self_documentation, and ship a substantially larger batch of high-leverage changes.
> You're allowed to be ambitious. Default to acting, not deliberating.

Eleven recommendations have working implementations and checks that fail against
the real 0.8.0 binary and pass against the integrated candidate. They cover
help/examples, flag suggestions, missing-path command hints, saved-report help
and errors, planner/worker prerequisites, machine-readable CLI discovery,
offline schemas, the embedded guide, readable inert plans and empty-file
language-schema correctness. The guide was expanded beyond its initial workflow
sections to include all 18 analyzers. Reviews also corrected missing catalog
version metadata, flag-value reflection, terminal-format controls and a
pre-existing partial-focus saved-report schema defect.

The implementation groups shared CLI primitives and their tests in one coherent
commit, rather than ten artificial commits with overlapping dependencies.
Separate correctness and performance changes have separate commits. This is a
deviation from the skill's suggested one-commit-per-recommendation bookkeeping;
the recommendation-to-test/evidence mapping remains explicit.

The additional prioritization round examined every remaining sub-500 score in
the four named dimensions. Three concern Go/Windows environment variables;
invalid runtime values may be handled before Go main runs, and a new override
layer would change existing semantics. The embedded reference explains their
scope and limitations. Plain `help` remains text; the explicit CLI catalog and
guide supply structured alternatives. Generic excess-path errors remain
bounded refusals; automatic multi-path interpretation would change source
selection. None justified expanding the command contract merely to increase a
score. No further production edit resulted from that prioritization round. Later independent review added a terminal-safe final error boundary, finite catalog choices and saved-report cause preservation; their checks are recorded separately.

The matched 108-surface cohort has a median paired increase of 195 rubric
points and no overall surface decrease greater than 50 points. The six newly
inventoried surfaces are excluded from that comparison; GOMEMLIMIT was an
existing runtime control newly documented here. Scores are judgments from
different pre/post reviewer cohorts, not a causal estimate of human or agent
productivity. Fresh-agent transcripts and executable regression checks provide
separate behavioral evidence.

The soft count target does not justify silent mode changes, breaking the 0/1
exit contract, adding a new command/path collision, or exposing a legacy
schema as more complete than it is. Remaining gaps and external review belong
in the handoff. This check does not certify a release.
