export interface ParsedSearch {
  text?: string;
  repo?: string;
  kind?: "issue" | "pull_request";
  state?: "open" | "closed";
  author?: string;
  isBot?: boolean;
  authorIsBot?: boolean;
}

/**
 * Parses advanced search syntax with GitHub/Octobox prefixes:
 * - repo:<owner/repo> or repo:"<owner/repo>"
 * - is:pr (kind=pull_request)
 * - is:issue (kind=issue)
 * - is:open (state=open)
 * - is:closed (state=closed)
 * - is:bot (authorIsBot=true, isBot=true)
 * - -is:bot or is:human (authorIsBot=false, isBot=false)
 * - author:<username> or author:"<username>"
 *
 * Words not matching recognized prefixes, unknown prefixes, or quoted phrases
 * are collected into `text`.
 */
export function parseSearchSyntax(input: string): ParsedSearch {
  const result: ParsedSearch = { text: "" };
  if (!input || !input.trim()) {
    return result;
  }

  const textTokens: string[] = [];
  let i = 0;
  const len = input.length;

  while (i < len) {
    // Skip whitespace
    while (i < len && /\s/.test(input.charAt(i))) {
      i++;
    }
    if (i >= len) break;

    const tokenStart = i;
    const prefixMatch = input.slice(i).match(/^(-?[a-zA-Z0-9_-]+):/);

    if (prefixMatch && prefixMatch[1]) {
      const prefix = prefixMatch[1];
      const prefixLen = prefixMatch[0].length;
      const valStart = i + prefixLen;

      let val: string | null = null;
      let valEnd = valStart;

      if (valStart < len && input.charAt(valStart) === '"') {
        let quoteEnd = valStart + 1;
        let foundQuote = false;
        while (quoteEnd < len) {
          if (input.charAt(quoteEnd) === "\\" && quoteEnd + 1 < len) {
            quoteEnd += 2;
            continue;
          }
          if (input.charAt(quoteEnd) === '"') {
            foundQuote = true;
            quoteEnd++;
            break;
          }
          quoteEnd++;
        }
        if (foundQuote) {
          val = input.slice(valStart + 1, quoteEnd - 1);
          valEnd = quoteEnd;
        } else {
          val = null;
          valEnd = len;
        }
      } else {
        while (valEnd < len && !/\s/.test(input.charAt(valEnd))) {
          valEnd++;
        }
        val = input.slice(valStart, valEnd);
      }

      if (val !== null && val.length > 0) {
        const prefixLower = prefix.toLowerCase();
        const valueLower = val.toLowerCase();
        let handled = false;

        if (prefixLower === "repo") {
          result.repo = val;
          handled = true;
        } else if (prefixLower === "is") {
          if (valueLower === "pr") {
            result.kind = "pull_request";
            handled = true;
          } else if (valueLower === "issue") {
            result.kind = "issue";
            handled = true;
          } else if (valueLower === "open") {
            result.state = "open";
            handled = true;
          } else if (valueLower === "closed") {
            result.state = "closed";
            handled = true;
          } else if (valueLower === "bot") {
            result.isBot = true;
            result.authorIsBot = true;
            handled = true;
          } else if (valueLower === "human") {
            result.isBot = false;
            result.authorIsBot = false;
            handled = true;
          }
        } else if (prefixLower === "-is") {
          if (valueLower === "bot") {
            result.isBot = false;
            result.authorIsBot = false;
            handled = true;
          }
        } else if (prefixLower === "author") {
          result.author = val;
          handled = true;
        }

        if (handled) {
          i = valEnd;
          continue;
        }
      }

      // If prefix wasn't handled or was malformed, consume token as text
      textTokens.push(input.slice(tokenStart, valEnd));
      i = valEnd;
      continue;
    }

    // Non-prefix token: check for quoted phrase
    if (input.charAt(i) === '"') {
      let quoteEnd = i + 1;
      let foundQuote = false;
      while (quoteEnd < len) {
        if (input.charAt(quoteEnd) === "\\" && quoteEnd + 1 < len) {
          quoteEnd += 2;
          continue;
        }
        if (input.charAt(quoteEnd) === '"') {
          foundQuote = true;
          quoteEnd++;
          break;
        }
        quoteEnd++;
      }
      if (foundQuote) {
        textTokens.push(input.slice(i, quoteEnd));
        i = quoteEnd;
      } else {
        textTokens.push(input.slice(i));
        i = len;
      }
      continue;
    }

    // Regular word
    let wordEnd = i;
    while (wordEnd < len && !/\s/.test(input.charAt(wordEnd))) {
      wordEnd++;
    }
    textTokens.push(input.slice(i, wordEnd));
    i = wordEnd;
  }

  result.text = textTokens.join(" ").trim();
  return result;
}

/**
 * Serializes a ParsedSearch object back into a canonical search syntax string.
 */
export function serializeSearchSyntax(parsed: ParsedSearch): string {
  const parts: string[] = [];

  if (parsed.repo) {
    const trimmed = parsed.repo.trim();
    if (trimmed) {
      parts.push(trimmed.includes(" ") ? `repo:"${trimmed}"` : `repo:${trimmed}`);
    }
  }

  if (parsed.kind === "pull_request") {
    parts.push("is:pr");
  } else if (parsed.kind === "issue") {
    parts.push("is:issue");
  }

  if (parsed.state === "open") {
    parts.push("is:open");
  } else if (parsed.state === "closed") {
    parts.push("is:closed");
  }

  const isBot = parsed.isBot ?? parsed.authorIsBot;
  if (isBot === true) {
    parts.push("is:bot");
  } else if (isBot === false) {
    parts.push("is:human");
  }

  if (parsed.author) {
    const trimmed = parsed.author.trim();
    if (trimmed) {
      parts.push(trimmed.includes(" ") ? `author:"${trimmed}"` : `author:${trimmed}`);
    }
  }

  if (parsed.text) {
    const trimmed = parsed.text.trim();
    if (trimmed) {
      parts.push(trimmed);
    }
  }

  return parts.join(" ").trim();
}
