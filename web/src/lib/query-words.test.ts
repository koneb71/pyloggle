import { describe, expect, it } from "vitest";
import { chips } from "./query";
import { describeChip } from "./query-words";

const say = (q: string) => chips(q).map(describeChip);

describe("describeChip", () => {
  it("reads field filters as sentences", () => {
    expect(say("service:api -env:dev level:error")).toEqual(["App is api", "Environment is not dev", "Severity is Error"]);
  });
  it("reads free text as a message search", () => {
    expect(say('"timed out" -boom')).toEqual(['Message contains "timed out"', `Message doesn't contain "boom"`]);
  });
  it("reads comparisons, groups, wildcards and attributes", () => {
    expect(say("http.status:>=500 duration_ms:>250")).toEqual(["Response code ≥ 500", "Duration > 250"]);
    expect(say("level:(error OR fatal)")).toEqual(["Severity is Error or Fatal"]);
    expect(say("msg:connect*")).toEqual(["Message starts with connect"]);
    expect(say("attrs.order_id:42 @region:eu tags.team:core")).toEqual(["Order ID is 42", "Region is eu", "Team is core"]);
    expect(say('http.route:"/v1/orders"')).toEqual(["Route is /v1/orders"]);
  });
});
