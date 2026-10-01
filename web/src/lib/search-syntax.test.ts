import { describe, expect, it } from "vitest";
import { parseSearchSyntax, serializeSearchSyntax, type ParsedSearch } from "./search-syntax";

describe("parseSearchSyntax", () => {
  it("handles empty or whitespace-only inputs", () => {
    expect(parseSearchSyntax("")).toEqual({ text: "" });
    expect(parseSearchSyntax("   \t  \n  ")).toEqual({ text: "" });
  });

  it("parses repo prefixes with and without quotes", () => {
    expect(parseSearchSyntax("repo:owner/repo")).toEqual({ repo: "owner/repo", text: "" });
    expect(parseSearchSyntax('repo:"owner/repo-name"')).toEqual({ repo: "owner/repo-name", text: "" });
    expect(parseSearchSyntax('repo:"owner with space/repo"')).toEqual({
      repo: "owner with space/repo",
      text: "",
    });
  });

  it("parses is:pr and is:issue into kind", () => {
    expect(parseSearchSyntax("is:pr")).toEqual({ kind: "pull_request", text: "" });
    expect(parseSearchSyntax("is:issue")).toEqual({ kind: "issue", text: "" });
  });

  it("parses is:open and is:closed into state", () => {
    expect(parseSearchSyntax("is:open")).toEqual({ state: "open", text: "" });
    expect(parseSearchSyntax("is:closed")).toEqual({ state: "closed", text: "" });
  });

  it("parses is:bot, -is:bot, and is:human into bot flags", () => {
    const bot = parseSearchSyntax("is:bot");
    expect(bot.isBot).toBe(true);
    expect(bot.authorIsBot).toBe(true);

    const minusBot = parseSearchSyntax("-is:bot");
    expect(minusBot.isBot).toBe(false);
    expect(minusBot.authorIsBot).toBe(false);

    const human = parseSearchSyntax("is:human");
    expect(human.isBot).toBe(false);
    expect(human.authorIsBot).toBe(false);
  });

  it("parses author prefix with and without quotes", () => {
    expect(parseSearchSyntax("author:octocat")).toEqual({ author: "octocat", text: "" });
    expect(parseSearchSyntax('author:"octo-cat"')).toEqual({ author: "octo-cat", text: "" });
    expect(parseSearchSyntax('author:"Alice Smith"')).toEqual({ author: "Alice Smith", text: "" });
  });

  it("preserves unhit words, unknown prefixes, and quoted phrases as text", () => {
    const res = parseSearchSyntax('fix "memory leak" is:unknown label:bug crash');
    expect(res.text).toBe('fix "memory leak" is:unknown label:bug crash');
  });

  it("parses comprehensive combination of prefixes and text", () => {
    const res = parseSearchSyntax(
      'repo:acme/sentinel is:pr is:open author:alice is:bot fix "high cpu usage"'
    );
    expect(res).toEqual({
      repo: "acme/sentinel",
      kind: "pull_request",
      state: "open",
      author: "alice",
      isBot: true,
      authorIsBot: true,
      text: 'fix "high cpu usage"',
    });
  });

  it("supports case-insensitive prefixes", () => {
    const res = parseSearchSyntax("REPO:acme/app IS:PR IS:OPEN AUTHOR:alice -IS:BOT");
    expect(res).toEqual({
      repo: "acme/app",
      kind: "pull_request",
      state: "open",
      author: "alice",
      isBot: false,
      authorIsBot: false,
      text: "",
    });
  });

  it("gracefully tolerates empty prefix values and unclosed quotes", () => {
    expect(parseSearchSyntax("repo:")).toEqual({ text: "repo:" });
    expect(parseSearchSyntax('repo:"')).toEqual({ text: 'repo:"' });
    expect(parseSearchSyntax('"unclosed phrase')).toEqual({ text: '"unclosed phrase' });
  });
});

describe("serializeSearchSyntax", () => {
  it("serializes empty search state to empty string", () => {
    expect(serializeSearchSyntax({ text: "" })).toBe("");
  });

  it("serializes structured fields in consistent order", () => {
    const parsed: ParsedSearch = {
      repo: "acme/sentinel",
      kind: "pull_request",
      state: "open",
      isBot: true,
      author: "alice",
      text: "fix bug",
    };
    expect(serializeSearchSyntax(parsed)).toBe(
      "repo:acme/sentinel is:pr is:open is:bot author:alice fix bug"
    );
  });

  it("quotes values containing spaces", () => {
    const parsed: ParsedSearch = {
      repo: "my org/my repo",
      kind: "issue",
      state: "closed",
      isBot: false,
      author: "Alice Smith",
      text: "memory leak",
    };
    expect(serializeSearchSyntax(parsed)).toBe(
      'repo:"my org/my repo" is:issue is:closed is:human author:"Alice Smith" memory leak'
    );
  });

  it("roundtrips with parseSearchSyntax", () => {
    const raw = "repo:Silentely/Repo-Sentinel is:pr is:open is:bot author:renovate fix dependency";
    const parsed = parseSearchSyntax(raw);
    const serialized = serializeSearchSyntax(parsed);
    expect(serialized).toBe(raw);
  });
});
