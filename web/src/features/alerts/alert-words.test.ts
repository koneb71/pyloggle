import { describe, expect, it } from "vitest";
import type { AlertRule } from "@/api/types";
import { formFromRule, ruleCondition, ruleFromForm, ruleRecipients, ruleScope, secondsWords } from "./alert-words";

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
