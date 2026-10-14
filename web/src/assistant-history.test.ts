import { test } from "node:test";
import assert from "node:assert/strict";
import { clipBytes, maxMessageBytes, parseStored, stored, toApi, type Msg } from "./assistant-history.ts";

const bytes = (s: string) => new TextEncoder().encode(s).length;

test("toApi notes changed task ids, not titles", () => {
  const m: Msg = {
    role: "assistant",
    content: "Done.",
    steps: [
      { tool: "search_tasks", summary: "Searched", tasks: [{ id: "a-1", title: "found" }] },
      { tool: "create_tasks", summary: "Created", tasks: [{ id: "a-2", title: "A very long title" }, { id: "a-3", title: "x" }] },
      { tool: "update_task", summary: "Updated", tasks: [{ id: "a-2", title: "again" }] },
      { tool: "comment", summary: "Failed", error: "nope", tasks: [{ id: "a-9", title: "y" }] },
    ],
  };
  assert.deepEqual(toApi(m), { role: "assistant", content: "Done.\n\n(Tasks I changed: a-2, a-3)" });
});

test("toApi keeps every message within the server's limit", () => {
  const many = Array.from({ length: 40 }, (_, i) => ({ id: `acme-${i}`, title: "t".repeat(200) }));
  const m: Msg = { role: "assistant", content: "é".repeat(6000), steps: [{ tool: "create_tasks", summary: "Created", tasks: many }] };
  const out = toApi(m).content;
  assert.ok(bytes(out) <= maxMessageBytes, `${bytes(out)} bytes`);
  assert.match(out, /\(Tasks I changed: acme-0, .*acme-9, and 30 more\)$/);
  assert.ok(bytes(toApi({ role: "user", content: "x".repeat(9000) }).content) <= maxMessageBytes);
});

test("clipBytes never splits a character", () => {
  assert.equal(clipBytes("aé€", 4), "aé");
  assert.equal(clipBytes("short", 100), "short");
});

test("a kept conversation only comes back for the same workspace and member", () => {
  const raw = stored("acme", "m1", [{ role: "user", content: "hi" }]);
  assert.equal(parseStored(raw, "acme", "m1").length, 1);
  assert.deepEqual(parseStored(raw, "acme", "m2"), []);
  assert.deepEqual(parseStored(raw, "globex", "m1"), []);
  assert.deepEqual(parseStored("not json", "acme", "m1"), []);
  assert.deepEqual(parseStored(null, "acme", "m1"), []);
});
