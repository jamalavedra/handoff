import { test } from "node:test";
import assert from "node:assert/strict";
import { safeNext } from "./next.ts";

test("safeNext keeps in-app paths", () => {
  for (const ok of ["/", "/tasks/acme-1", "/team?task=acme-2#c", "/settings/agents"]) assert.equal(safeNext(ok), ok);
});

test("safeNext refuses anything that can leave the site or run script", () => {
  for (const bad of [null, undefined, "", "javascript:alert(1)", "JavaScript:alert(1)", "https://evil.com", "//evil.com", "/\\evil.com", "\\\\evil.com", "/\t/evil.com", "/\n/evil.com", "/\u007f/x", "data:text/html,x", "tasks"])
    assert.equal(safeNext(bad), "/", String(bad));
});
