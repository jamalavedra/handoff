import { test } from "node:test";
import assert from "node:assert/strict";
import { daysLeft, directionsURL, distance, duration, googleCalendarURL, ics, initials, localDate, network, when, type Reminder } from "./artifacts.ts";

test("durations and distances read short", () => {
  assert.equal(duration(20), "1 min");
  assert.equal(duration(3100), "52 min");
  assert.equal(duration(3600), "1 h");
  assert.equal(duration(3900), "1 h 5 min");
  assert.equal(distance(850.4), "850 m");
  assert.equal(distance(4321), "4.3 km");
  assert.equal(distance(25400), "25 km");
});

test("directions open the same trip and mode", () => {
  assert.equal(
    directionsURL({ lat: 1.5, lng: 2 }, { lat: 3, lng: -4 }, "bike"),
    "https://www.google.com/maps/dir/?api=1&origin=1.5,2&destination=3,-4&travelmode=bicycling",
  );
});

test("reminder dates are local and relative", () => {
  const now = new Date(2026, 9, 19, 15, 0);
  const d = localDate("2026-10-24T09:30");
  assert.deepEqual([d.getFullYear(), d.getMonth(), d.getDate(), d.getHours(), d.getMinutes()], [2026, 9, 24, 9, 30]);
  assert.equal(daysLeft({ title: "x", at: "2026-10-24", all_day: true }, now), 5);
  assert.equal(daysLeft({ title: "x", at: "2026-10-18", all_day: true }, now), -1);
  assert.equal(when({ title: "x", at: "2026-10-19", all_day: true }, now), "Today");
  assert.equal(when({ title: "x", at: "2026-10-20", all_day: true }, now), "Tomorrow");
  assert.match(when({ title: "x", at: "2026-10-20T09:30", all_day: false }, now), /^Tomorrow, 9:30/);
  assert.match(when({ title: "x", at: "2027-01-05", all_day: true }, now), /2027/);
});

test("a reminder makes a calendar file with an alarm", () => {
  const day: Reminder = { title: "Order; the, costume", at: "2026-10-24", all_day: true, note: "line1\nline2" };
  const f = ics(day, "t-1", new Date(Date.UTC(2026, 9, 19, 12, 0, 0)));
  assert.ok(f.includes("DTSTART;VALUE=DATE:20261024\r\n"));
  assert.ok(f.includes("DTEND;VALUE=DATE:20261025\r\n"));
  assert.ok(f.includes("SUMMARY:Order\\; the\\, costume\r\n"));
  assert.ok(f.includes("DESCRIPTION:line1\\nline2\r\n"));
  assert.ok(f.includes("DTSTAMP:20261019T120000Z\r\n"));
  assert.ok(f.includes("BEGIN:VALARM"));
  const timed = ics({ title: "Call", at: "2026-10-24T23:45", all_day: false }, "t-2");
  assert.ok(timed.includes("DTSTART:20261024T234500\r\n"));
  assert.ok(timed.includes("DTEND:20261025T001500\r\n"));
});

test("google calendar gets the same dates", () => {
  const u = new URL(googleCalendarURL({ title: "Order", at: "2026-10-24", all_day: true }));
  assert.equal(u.searchParams.get("dates"), "20261024/20261025");
  assert.equal(u.searchParams.get("text"), "Order");
  assert.equal(new URL(googleCalendarURL({ title: "Call", at: "2026-10-24T09:00", all_day: false })).searchParams.get("dates"), "20261024T090000/20261024T093000");
});

test("people get initials and network actions", () => {
  assert.equal(initials("jane  doe smith"), "JD");
  assert.equal(initials(""), "?");
  assert.equal(network("linkedin").action, "Connect");
  assert.equal(network("unknown").label, "Website");
});
