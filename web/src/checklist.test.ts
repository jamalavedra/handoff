import { test } from "node:test";
import assert from "node:assert/strict";
import { progress, sources, toggleItem } from "./checklist.ts";

const md = `Get it sorted early.

- [ ] Check what's trending
- [x] Pick a direction
\`\`\`
- [ ] not an item
\`\`\`
1. [ ] Order: by Oct 24
   - [ ] Try it on`;

test("ticks the nth item, skipping code", () => {
  assert.match(toggleItem(md, 0), /- \[x\] Check what's trending/);
  assert.match(toggleItem(md, 1), /- \[ \] Pick a direction/);
  assert.match(toggleItem(md, 2), /1\. \[x\] Order: by Oct 24/);
  assert.match(toggleItem(md, 3), / {3}- \[x\] Try it on/);
  assert.match(toggleItem(md, 3), /- \[ \] not an item/);
  assert.equal(toggleItem(md, 9), md);
});

test("counts progress", () => {
  assert.deepEqual(progress(md), { total: 4, done: 1 });
  assert.deepEqual(progress("no list"), { total: 0, done: 0 });
});

test("lists cited sites once", () => {
  const s = sources("See [Frightgeist](https://frightgeist.withgoogle.com/x) and https://www.blog.google/a, again https://frightgeist.withgoogle.com/y.");
  assert.deepEqual(s.map((x) => x.host), ["frightgeist.withgoogle.com", "blog.google"]);
});
