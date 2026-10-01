import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import {
  PixelTree,
  PixelHouse,
  PixelFlowerPatch,
  PixelCloud,
  PixelCampfire,
  PixelMailbox,
  PixelWatchtower,
  PixelSheep,
  PixelCow,
  PixelMountain,
  PixelStream,
  PixelFence,
  PixelAuthFlanks,
  PixelPeacefulClearing,
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

    const { container: fireContainer } = render(<PixelCampfire />);
    const fireSvg = fireContainer.querySelector("svg");
    expect(fireSvg).not.toBeNull();
    expect(fireSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(fireSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: mailContainer } = render(<PixelMailbox />);
    const mailSvg = mailContainer.querySelector("svg");
    expect(mailSvg).not.toBeNull();
    expect(mailSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(mailSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: towerContainer } = render(<PixelWatchtower />);
    const towerSvg = towerContainer.querySelector("svg");
    expect(towerSvg).not.toBeNull();
    expect(towerSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(towerSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: sheepContainer } = render(<PixelSheep variant="grazing" />);
    const sheepSvg = sheepContainer.querySelector("svg");
    expect(sheepSvg).not.toBeNull();
    expect(sheepSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(sheepSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: cowContainer } = render(<PixelCow facing="left" />);
    const cowSvg = cowContainer.querySelector("svg");
    expect(cowSvg).not.toBeNull();
    expect(cowSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(cowSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: mountainContainer } = render(<PixelMountain />);
    const mountainSvg = mountainContainer.querySelector("svg");
    expect(mountainSvg).not.toBeNull();
    expect(mountainSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(mountainSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: streamContainer } = render(<PixelStream />);
    const streamSvg = streamContainer.querySelector("svg");
    expect(streamSvg).not.toBeNull();
    expect(streamSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(streamSvg?.getAttribute("shape-rendering")).toBe("crispEdges");

    const { container: fenceContainer } = render(<PixelFence />);
    const fenceSvg = fenceContainer.querySelector("svg");
    expect(fenceSvg).not.toBeNull();
    expect(fenceSvg?.getAttribute("aria-hidden")).toBe("true");
    expect(fenceSvg?.getAttribute("shape-rendering")).toBe("crispEdges");
  });

  it("renders compound scenery ground, clearing, flanks, and sidebar with aria-hidden", () => {
    const { container: groundContainer } = render(<PixelSceneryGround />);
    const ground = groundContainer.querySelector(".pixel-scenery-ground");
    expect(ground).not.toBeNull();
    expect(ground?.getAttribute("aria-hidden")).toBe("true");

    const { container: clearingContainer } = render(<PixelPeacefulClearing />);
    const clearing = clearingContainer.querySelector(".pixel-peaceful-clearing");
    expect(clearing).not.toBeNull();
    expect(clearing?.getAttribute("aria-hidden")).toBe("true");

    const { container: sidebarContainer } = render(<PixelScenerySidebarDecor />);
    const sidebar = sidebarContainer.querySelector(".pixel-scenery-sidebar");
    expect(sidebar).not.toBeNull();
    expect(sidebar?.getAttribute("aria-hidden")).toBe("true");

    const { container: flanksContainer } = render(<PixelAuthFlanks />);
    const flanks = flanksContainer.querySelector(".auth-flanks");
    expect(flanks).not.toBeNull();
    expect(flanks?.getAttribute("aria-hidden")).toBe("true");
  });
});
