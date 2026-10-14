import { test } from "node:test";
import assert from "node:assert/strict";
import { Window } from "happy-dom";

// Tiptap parses markdown through the DOM, so give node one before loading the editor.
const win = new Window();
const g = globalThis as Record<string, unknown>;
for (const k of ["window", "document", "navigator", "DOMParser", "Node", "HTMLElement", "Element", "getComputedStyle", "MutationObserver"]) g[k] ??= (win as unknown as Record<string, unknown>)[k];
const { Editor } = await import("@tiptap/core");
const { richExtensions, toMarkdown } = await import("./richtext.ts");

const load = (md: string) => new Editor({ extensions: richExtensions(), content: md });
const roundTrip = (md: string) => toMarkdown(load(md));

// What an agent typically leaves on a task: headings, a GFM comparison table, a checklist, lists,
// code, quotes and links. It must come back byte for byte.
const agentDoc = `# Research: vector databases

Compared **three** options for *our* use case. See [the docs](https://example.com/a_b) and \`pgvector\`.

## Comparison

| Option | Cost | Notes |
| --- | --- | --- |
| pgvector | $0 | Runs in **Postgres** |
| Pinecone | $70/mo | Managed, \`p1\` pods |
| Qdrant | $25/mo | Rust, [self-host](https://qdrant.tech) |

## Next steps

- [x] Benchmark recall
- [ ] Ask @Joan about the budget
  - [ ] Nested check

1. First
2. Second
   - nested bullet

> Quote with *emphasis*

#### Details

~~Dropped~~ Weaviate. ![chart](https://example.com/chart.png)

\`\`\`sql
select * from items where a < 3;
\`\`\`

---

Plain line with snake_case_name, 50% off, a < b and a URL https://x.com/a_b_c`;

test("an agent-written document survives load and save unchanged", () => {
  assert.equal(roundTrip(agentDoc), agentDoc);
});

test("saving twice is stable", () => {
  const once = roundTrip("Some _italic_ and __bold__\n\n* star bullet\n* another\n\n- [ ] a\n- [x] b");
  // A checklist right after a bullet list gets the other marker, or the two would merge when read back.
  assert.equal(once, "Some *italic* and **bold**\n\n- star bullet\n- another\n\n* [ ] a\n* [x] b");
  assert.equal(roundTrip(once), once);
});

test("edits serialize to markdown", () => {
  const e = load("- [ ] ship it\n\nhello");
  // Tick the checkbox the way a click does.
  e.state.doc.descendants((node, pos) => {
    if (node.type.name === "taskItem") e.commands.command(({ tr }) => { tr.setNodeMarkup(pos, undefined, { ...node.attrs, checked: true }); return true; });
  });
  e.commands.setTextSelection({ from: e.state.doc.content.size - 1, to: e.state.doc.content.size - 1 });
  e.commands.insertContent(" world");
  e.commands.setTextSelection({ from: e.state.doc.content.size - 6, to: e.state.doc.content.size - 1 });
  e.commands.toggleBold();
  assert.equal(toMarkdown(e), "- [x] ship it\n\nhello **world**");
});

test("tables keep pipes and line breaks inside one row", () => {
  const e = load("| a | b |\n| --- | --- |\n| x \\| y | z |");
  assert.equal(toMarkdown(e), "| a | b |\n| --- | --- |\n| x \\| y | z |");
  e.commands.setTextSelection(5); // after "a" in the first header cell
  e.commands.setHardBreak();
  e.commands.insertContent("c");
  assert.equal(toMarkdown(e), "| a c | b |\n| --- | --- |\n| x \\| y | z |");
});

test("a list mixing tasks and plain items becomes separate lists", () => {
  const once = roundTrip("- a\n- [ ] b\n- [x] c\n- d");
  assert.equal(once, "- a\n\n* [ ] b\n* [x] c\n\n- d");
  assert.equal(roundTrip(once), once);
});

test("empty body stays empty", () => {
  assert.equal(roundTrip(""), "");
});

test("blank lines inside a code fence are kept", () => {
  const doc = "Intro\n\n```\na\n\n\n\nb\n```\n\n- item\n\n  ```js\n  x\n  \n  \n  y\n  ```";
  assert.equal(roundTrip(doc), doc);
  const e = load(doc);
  e.commands.setTextSelection(6); // after "Intro"
  e.commands.insertContent("!");
  assert.equal(toMarkdown(e), doc.replace("Intro", "Intro!"));
});
