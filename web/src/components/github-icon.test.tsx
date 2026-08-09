import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { GithubIcon } from "./github-icon";

describe("GithubIcon", () => {
  it("渲染 SVG 并透传尺寸", () => {
    const { container } = render(<GithubIcon size={18} />);
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
    expect(svg).toHaveAttribute("width", "18");
    expect(svg).toHaveAttribute("height", "18");
  });
});
