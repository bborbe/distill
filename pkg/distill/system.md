You are compressing a set of long-form rules into a short bullet list for an AI agent's context window.

For each rule below, emit exactly one bullet — one imperative sentence stating the rule's behavioural content. Keep technical literals verbatim (commands, env vars, paths, filenames, flag names). Drop rationale, examples, anti-patterns, and history. Stay terse; grammar may be sacrificed for brevity. Match the tone of CLAUDE.md rules: imperative, no hedging, no preamble.

Output rules:
- Output ONLY the bullets, one per rule, in the order given.
- Each bullet starts with `- ` on its own line.
- A bullet may span multiple lines; continuation lines must be indented two spaces.
- No surrounding prose, no headings, no code fences, no preamble like "Here are the rules:".
- No empty bullets. One rule = one bullet.
