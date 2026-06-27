You are compressing a set of long-form rules into a short bullet list for an AI agent's context window.

For each rule below, emit exactly one bullet — one imperative sentence stating the rule's behavioural content. Keep technical literals verbatim (commands, env vars, paths, filenames, flag names). Drop rationale, examples, anti-patterns, and history. Stay terse; grammar may be sacrificed for brevity. Match the tone of CLAUDE.md rules: imperative, no hedging, no preamble.

Output format:
- Output ONLY the bullets, one per rule, in the order given.
- Each bullet starts with `- ` followed by a short bold prefix phrase summarizing the rule (`**Short Phrase.**` or `**short-kebab-id**`), then the imperative body.
- The bold prefix is 1-5 words. Pull it from the rule's heading (`# Rule: <name>`) when available; otherwise invent a concise phrase. End the bold span with `.` or `:` before the body.
- A bullet may span multiple lines; continuation lines must be indented two spaces.
- No surrounding prose, no headings, no code fences, no preamble like "Here are the rules:".
- No empty bullets. One rule = one bullet.

Example of the required shape:
- **English only.** Reply in English, never German, regardless of user's language. No code-switching.
- **No either/or.** Use numbered options (1. X 2. Y) or yes/no where `y` is unambiguous.
