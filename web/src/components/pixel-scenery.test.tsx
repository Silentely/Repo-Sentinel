import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import {
  PixelTree,
  PixelHouse,
  PixelFlowerPatch,
  PixelCloud,
  PixelSceneryGround,
  PixelScenerySidebarDecor,
} from "./pixel-scenery";

describe("PixelScenery Components", () => {
  it("renders SVG elements with crispEdges and aria-hidden", () => {
    const { container: treeContainer } = render(<PixelTree />);
    const treeSvg = treeContainer.querySelector("svg");
    expect(treeSvg).not.toBeNull();
    expect(treeSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(treeSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: houseContainer } = render(<PixelHouse />);
    const houseSvg = houseContainer.querySelector("svg");
    expect(houseSvg).not.toBeNull();
    expect(houseSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(houseSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: flowerContainer } = render(<PixelFlowerPatch />);
    const flowerSvg = flowerContainer.querySelector("svg");
    expect(flowerSvg).not.toBeNull();
    expect(flowerSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(flowerSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: cloudContainer } = render(<PixelCloud />);
    const cloudSvg = cloudContainer.querySelector("svg");
    expect(cloudSvg).not.toBeNull();
    expect(cloudSvg?.getAttribute("aria-hidden")).toBe("true");
  });

  it("renders compound scenery ground and sidebar with aria-hidden", () => {
    const { container: groundContainer } = render(<PixelSceneryGround />);
    const ground = groundContainer.querySelector(".pixel-scenery-ground");
    expect(ground).not.toBeNull();
    expect(ground?.getAttribute("aria-hidden")).toBe("true");

    const { container: sidebarContainer } = render(<PixelScenerySidebarDecor />);
    const sidebar = sidebarContainer.querySelector(".pixel-scenery-sidebar");
    expect(sidebar).not.toBeNull();
    expect(sidebar?.getAttribute("aria-hidden")).toBe("true");
  });
});
