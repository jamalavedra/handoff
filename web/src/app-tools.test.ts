import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { knownTools, toolLabel } from "./app-tools.ts";

test("known tools read as what they let the assistant do", () => {
  assert.deepEqual(toolLabel("GMAIL_FETCH_EMAILS", "gmail"), { text: "Search and read emails", icon: "search" });
  assert.deepEqual(toolLabel("GOOGLECALENDAR_FIND_FREE_SLOTS", "googlecalendar"), { text: "Find free time", icon: "free" });
});

test("a tool the map doesn't know still reads sensibly", () => {
  assert.deepEqual(toolLabel("GMAIL_LIST_LABELS", "gmail"), { text: "List labels", icon: "list" });
  assert.deepEqual(toolLabel("ODD", "gmail"), { text: "Odd", icon: "list" });
});

test("every tool the server's catalog chooses has a label", () => {
  const src = readFileSync(new URL("../../internal/core/apps.go", import.meta.url), "utf8");
  const catalog = src.slice(src.indexOf("var appCatalog"), src.indexOf("// maxAppTools"));
  const chosen = [...catalog.matchAll(/"([A-Z][A-Z0-9_]+)"/g)].map((m) => m[1]);
  assert.ok(chosen.length > 30, `parsed ${chosen.length} tools from the catalog`);
  assert.deepEqual(chosen.filter((t) => !knownTools.includes(t)), []);
});
