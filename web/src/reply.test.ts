import { test } from "node:test";
import assert from "node:assert/strict";
import { cleanQuote, replyArgs, selected, snippet } from "./reply.ts";

test("quotes fold to one line and clip like the server", () => {
  assert.equal(cleanQuote("  the night\n\n bus  "), "the night bus");
  const long = cleanQuote("x".repeat(600));
  assert.equal([...long].length, 500);
  assert.ok(long.endsWith("…"));
  assert.equal(cleanQuote("ab", 5), "ab");
  assert.equal(cleanQuote("😀".repeat(10), 4), "😀😀😀…");
});

test("snippets drop markdown", () => {
  assert.equal(snippet("**Option A** is [the train](https://x.y)\n# next"), "Option A is the train next");
});

test("reply args only carry a quote when there is one", () => {
  assert.deepEqual(replyArgs(null), {});
  assert.deepEqual(replyArgs({ id: "c1", excerpt: "hi" }), { reply_to: "c1" });
  assert.deepEqual(replyArgs({ id: "c1", excerpt: "hi", quote: "h" }), { reply_to: "c1", quote: "h" });
});

test("a selection replies only when it lies in one message", () => {
  assert.deepEqual(selected("c1", "c1", " bus\n"), { id: "c1", quote: "bus" });
  assert.equal(selected("c1", "c2", "bus"), null);
  assert.equal(selected(null, null, "bus"), null);
  assert.equal(selected("c1", "c1", "  \n "), null);
});
