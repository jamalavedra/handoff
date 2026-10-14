import { test } from "node:test";
import assert from "node:assert/strict";
import { appendSpoken, dictationError, level, speechLang, transcript } from "./dictation.ts";

const result = (text: string, isFinal: boolean) => Object.assign([{ transcript: text }], { isFinal });

test("splits settled words from the live guess", () => {
  assert.deepEqual(transcript([]), { final: "", interim: "" });
  assert.deepEqual(transcript([result("send contract", false)]), { final: "", interim: "send contract" });
  assert.deepEqual(transcript([result("Send contract to legal", true), result(" tomorrow  hash", false)]), {
    final: "Send contract to legal", interim: "tomorrow hash",
  });
  assert.deepEqual(transcript([result("one", true), result(" two", true)]), { final: "one two", interim: "" });
  assert.deepEqual(transcript([Object.assign([], { isFinal: true })]), { final: "", interim: "" });
});

test("appends spoken words after typed text", () => {
  assert.equal(appendSpoken("", "call Marta"), "call Marta");
  assert.equal(appendSpoken("   ", "call Marta"), "call Marta");
  assert.equal(appendSpoken("#Sales", "call Marta"), "#Sales call Marta");
  assert.equal(appendSpoken("#Sales ", "call Marta"), "#Sales call Marta");
  assert.equal(appendSpoken("#Sales", "  "), "#Sales");
});

test("picks the page language first", () => {
  assert.equal(speechLang("es", "en-GB"), "es");
  assert.equal(speechLang("", "en-GB"), "en-GB");
  assert.equal(speechLang(undefined, undefined), "en-US");
});

test("errors worth a toast", () => {
  assert.equal(dictationError("aborted"), null);
  assert.match(dictationError("not-allowed")!, /blocked/);
  assert.match(dictationError("service-not-allowed")!, /blocked/);
  assert.match(dictationError("no-speech")!, /catch/);
  assert.match(dictationError("network")!, /network/);
  assert.match(dictationError("bad-grammar")!, /unexpectedly/);
});

test("audio level", () => {
  assert.equal(level([]), 0);
  assert.equal(level(new Uint8Array(64).fill(128)), 0);
  assert.equal(level(new Uint8Array(64).fill(255)), 1);
  const quiet = level(Uint8Array.from({ length: 64 }, (_, i) => (i % 2 ? 136 : 120)));
  assert.ok(quiet > 0.2 && quiet < 0.3, String(quiet));
});
