# Domain vocabulary

## Corpus

The authored set of portable Markdown documents governed by one `poolboy.toml`.

## Logical locator

A canonical root-absolute corpus path such as `/guide.md`. A locator names where a document is addressed; it is not proof of particular bytes and does not survive a rename automatically.

## Content revision

The lowercase SHA-256 of exact document or artifact bytes, paired with their byte count. A revision identifies bytes, not truth, approval, or a stable document identity.

## Workspace

The private mutable project containing authored Markdown, source evidence, generated-file ownership, and review decisions.

## Publication

An immutable set of static files described by one `graph.json`. A signed graph authenticates exact published bytes and publisher identity; it does not establish prose correctness or permission.

## Review plan

A regenerable view of maintenance candidates derived from current evidence. It is not durable authority.

## Review decision

A durable private choice about a candidate, bound to exact document and evidence revisions. Changed preconditions make the decision stale.

## Reconciliation

A durable record that exact reviewed document bytes were considered against exact evidence bytes. Reconciliation does not claim semantic truth and does not advance the project-wide source baseline.
