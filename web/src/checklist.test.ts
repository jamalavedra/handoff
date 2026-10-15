import { test } from "node:test";
import assert from "node:assert/strict";
import { sources } from "./checklist.ts";

test("lists cited sites once", () => {
  const s = sources("See [Frightgeist](https://frightgeist.withgoogle.com/x) and https://www.blog.google/a, again https://frightgeist.withgoogle.com/y.");
  assert.deepEqual(s.map((x) => x.host), ["frightgeist.withgoogle.com", "blog.google"]);
});
