import { describe, expect, it } from "vitest";
import type { AlertRule } from "@/api/types";
import type { AlertFiring } from "@/api/types";
import { firingSentence, formFromRule, hourWords, ruleRepeat, ruleSentence, timeZones, ruleCondition, ruleFromForm, ruleChannels, ruleRecipients, ruleScope, secondsWords, usesDefaultSlack } from "./alert-words";

const rule = (over: Partial<AlertRule>): AlertRule => ({ id: "r", project_id: "p", name: "n", description: "", service: "", env: "", http_route: "", http_method: "", condition_type: "error_rate", threshold: 0.05, time_window_seconds: 300, cooldown_seconds: 900, enabled: true, notification_channels: [], notification_emails: [], webhook_urls: [], telegram_chat_ids: [], created_at: "", updated_at: "", ...over });

describe("alert rules in words", () => {
  it("describes each kind", () => {
    expect(ruleCondition(rule({ threshold: 0.05 }))).toBe("errors happen more than 3 times a minute");
    expect(ruleCondition(rule({ condition_type: "issue_spike", threshold: 20 }))).toBe("one problem happens 20 more times than in the 5 minutes before");
    expect(ruleCondition(rule({ condition_type: "regression" }))).toBe("a problem marked as fixed comes back");
    expect(ruleCondition(rule({ condition_type: "log_query", query: "level:error", comparison: "lt", threshold: 1 }))).toBe('fewer than 1 message matches "level:error"');
  });
  it("describes scope and recipients", () => {
    expect(ruleScope(rule({}))).toBe("in all apps");
    expect(ruleScope(rule({ service: "api", env: "prod", http_method: "POST", http_route: "/orders" }))).toBe("in api / prod, POST /orders");
    expect(ruleRecipients(rule({ notification_emails: ["a@x", "b@x"], slack_webhook_urls: ["https://hooks"], webhook_urls: ["https://w"] }))).toBe("e-mails 2 people · Slack · 1 webhook");
    expect(ruleRecipients(rule({}))).toBe("nobody yet");
    expect(secondsWords(3600)).toBe("1 hour");
    expect(secondsWords(90)).toBe("90 seconds");
  });
  it("round-trips the form's minutes and per-minute rates", () => {
    const r = rule({ threshold: 0.05, time_window_seconds: 600, cooldown_seconds: 1800 });
    const f = formFromRule(r);
    expect(f).toEqual({ windowMinutes: 10, cooldownMinutes: 30, threshold: 3 });
    expect(ruleFromForm("error_rate", f)).toEqual({ threshold: 0.05, time_window_seconds: 600, cooldown_seconds: 1800 });
    const s = rule({ condition_type: "issue_spike", threshold: 20 });
    expect(ruleFromForm("issue_spike", formFromRule(s)).threshold).toBe(20);
  });
});

describe("alerts sent", () => {
  it("shows the sentence the notification carried, when the payload has one", () => {
    const f = { condition_type: "error_rate", payload: { context: { summary: " 42 errors in 5 minutes. " } } } as unknown as AlertFiring;
    expect(firingSentence(f)).toBe("42 errors in 5 minutes.");
    expect(firingSentence({ condition_type: "error_rate", payload: {} } as unknown as AlertFiring)).toBeUndefined();
  });
});

describe("how often a rule alerts", () => {
  it("says each kind's once-only behavior in words", () => {
    expect(ruleRepeat("new_issue")).toBe("each problem alerts once");
    expect(ruleRepeat("regression")).toBe("each comeback alerts once");
    expect(ruleRepeat("issue_spike")).toBe("each problem alerts at most once a day");
    expect(ruleRepeat("error_rate")).toBe("alerts when it starts, again only after it recovers");
    expect(ruleRepeat("log_query")).toBe(ruleRepeat("error_rate"));
  });
});

describe("recap of yesterday", () => {
  const base = { threshold: 0, query: "", comparison: "", time_window_seconds: 86400, http_route: "/x", http_method: "GET" };
  it("reads as a schedule, not a condition", () => {
    expect(ruleSentence({ ...base, condition_type: "daily_recap", send_hour: 8, timezone: "Asia/Manila", service: "", env: "" })).toBe(
      "Sends a recap of yesterday every day at 08:00 (Asia/Manila), for all apps.",
    );
    expect(ruleSentence({ ...base, condition_type: "daily_recap", send_hour: 0, timezone: "", service: "api", env: "prod" })).toBe("Sends a recap of yesterday every day at 00:00 (UTC), for api / prod.");
    expect(ruleSentence({ ...base, condition_type: "new_issue", service: "", env: "", http_route: "", http_method: "" })).toBe("Alert when a new problem appears in all apps.");
    expect(ruleRepeat("daily_recap")).toBe("");
  });
  it("formats hours and lists UTC first", () => {
    expect(hourWords(7)).toBe("07:00");
    expect(hourWords(23)).toBe("23:00");
    expect(timeZones()[0]).toBe("UTC");
  });
});

describe("alert channels", () => {
  const none = { emails: [], webhooks: [], telegram: [], slack: [], slackDefault: false };
  it("follows the filled-in fields", () => {
    expect(ruleChannels(none)).toEqual([]);
    expect(ruleChannels({ ...none, emails: ["a@example.com"], telegram: ["-100"] })).toEqual(["email", "telegram"]);
    expect(ruleChannels({ ...none, slack: ["https://hooks.slack.com/services/x"] })).toEqual(["slack"]);
  });
  it("sends to the server's Slack webhook when it is ticked", () => {
    expect(ruleChannels({ ...none, slackDefault: true })).toEqual(["slack"]);
    expect(usesDefaultSlack({ notification_channels: ["slack"], slack_webhook_urls: [] })).toBe(true);
    expect(usesDefaultSlack({ notification_channels: ["slack"], slack_webhook_urls: ["https://hooks.slack.com/services/x"] })).toBe(false);
    expect(usesDefaultSlack({ notification_channels: ["email"], slack_webhook_urls: [] })).toBe(false);
  });
  it("names Slack on the rule card for either kind", () => {
    expect(ruleRecipients({ notification_emails: [], webhook_urls: [], telegram_chat_ids: [], slack_webhook_urls: [], notification_channels: ["slack"] })).toBe("Slack");
  });
});
