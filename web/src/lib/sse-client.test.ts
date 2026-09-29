import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  createDebouncedInvalidator,
  SSEManager,
  type SSEEventPayload,
} from "./sse-client";

describe("createDebouncedInvalidator", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("batches 50 rapid events into a single query invalidation batch after 150ms", () => {
    const queryClient = new QueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries").mockResolvedValue();

    const invalidator = createDebouncedInvalidator(queryClient, 150);

    for (let i = 0; i < 50; i++) {
      invalidator.schedule("events.created");
    }

    // Immediately, no invalidations should have happened yet
    expect(invalidateSpy).not.toHaveBeenCalled();

    // Advance 100ms (not yet 150ms)
    vi.advanceTimersByTime(100);
    expect(invalidateSpy).not.toHaveBeenCalled();

    // Advance past 150ms
    vi.advanceTimersByTime(60);
    expect(invalidateSpy).toHaveBeenCalled();

    // Verify invalidation called with active refetchType
    const calls = invalidateSpy.mock.calls;
    expect(calls.some(([arg]) => JSON.stringify(arg?.queryKey) === JSON.stringify(["dashboard"]))).toBe(true);
    expect(calls.some(([arg]) => JSON.stringify(arg?.queryKey) === JSON.stringify(["events"]))).toBe(true);

    // Ensure it was batched together in this single timer tick
    const totalCalls = invalidateSpy.mock.calls.length;
    vi.advanceTimersByTime(300);
    expect(invalidateSpy).toHaveBeenCalledTimes(totalCalls);
  });

  it("routes outbox.changed and ai_review.changed to appropriate query keys", () => {
    const queryClient = new QueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries").mockResolvedValue();

    const invalidator = createDebouncedInvalidator(queryClient, 150);

    invalidator.schedule("outbox.changed");
    invalidator.schedule("ai_review.changed");

    vi.advanceTimersByTime(160);

    const calls = invalidateSpy.mock.calls;
    expect(calls.some(([arg]) => JSON.stringify(arg?.queryKey) === JSON.stringify(["outbox"]))).toBe(true);
    expect(calls.some(([arg]) => JSON.stringify(arg?.queryKey) === JSON.stringify(["dashboard"]))).toBe(true);
  });
});

describe("SSEManager circuit breaker & lifecycle", () => {
  let mockEventSourceInstances: MockEventSource[] = [];

  class MockEventSource {
    url: string;
    withCredentials?: boolean;
    listeners: Record<string, ((event: any) => void)[]> = {};
    onerror: ((err: any) => void) | null = null;
    onopen: (() => void) | null = null;
    readyState = 0; // CONNECTING

    constructor(url: string, init?: { withCredentials?: boolean }) {
      this.url = url;
      this.withCredentials = init?.withCredentials;
      mockEventSourceInstances.push(this);
    }

    addEventListener(event: string, handler: (event: any) => void) {
      this.listeners[event] = this.listeners[event] || [];
      this.listeners[event].push(handler);
    }

    removeEventListener(event: string, handler: (event: any) => void) {
      if (this.listeners[event]) {
        this.listeners[event] = this.listeners[event].filter((h) => h !== handler);
      }
    }

    emit(event: string, data: any) {
      const handlers = this.listeners[event] || [];
      for (const handler of handlers) {
        handler({ data: JSON.stringify(data) });
      }
    }

    close() {
      this.readyState = 2; // CLOSED
    }
  }

  beforeEach(() => {
    vi.useFakeTimers();
    mockEventSourceInstances = [];
    (globalThis as any).EventSource = MockEventSource;
  });

  afterEach(() => {
    vi.useRealTimers();
    delete (globalThis as any).EventSource;
  });

  it("opens EventSource with credentials when started", () => {
    const queryClient = new QueryClient();
    const manager = new SSEManager(queryClient);

    manager.start();
    expect(mockEventSourceInstances.length).toBe(1);
    expect(mockEventSourceInstances[0]!.url).toBe("/api/v1/events/stream");
    expect(mockEventSourceInstances[0]!.withCredentials).toBe(true);

    manager.stop();
    expect(mockEventSourceInstances[0]!.readyState).toBe(2);
  });

  it("trips circuit breaker after 4 consecutive reconnect failures", () => {
    const queryClient = new QueryClient();
    const onCircuitBreak = vi.fn();
    const manager = new SSEManager(queryClient, {
      maxRetries: 4,
      onCircuitBreak,
    });

    manager.start();
    expect(mockEventSourceInstances.length).toBe(1);

    // Fail 1
    mockEventSourceInstances[0]!.onerror?.(new Event("error"));
    vi.advanceTimersByTime(2000);
    expect(mockEventSourceInstances.length).toBe(2);

    // Fail 2
    mockEventSourceInstances[1]!.onerror?.(new Event("error"));
    vi.advanceTimersByTime(4000);
    expect(mockEventSourceInstances.length).toBe(3);

    // Fail 3
    mockEventSourceInstances[2]!.onerror?.(new Event("error"));
    vi.advanceTimersByTime(8000);
    expect(mockEventSourceInstances.length).toBe(4);

    // Fail 4 -> should trip circuit breaker and not reconnect again
    mockEventSourceInstances[3]!.onerror?.(new Event("error"));
    vi.advanceTimersByTime(60000);

    expect(onCircuitBreak).toHaveBeenCalledTimes(1);
    // Should still be 4 instances (no 5th attempt)
    expect(mockEventSourceInstances.length).toBe(4);

    manager.stop();
  });

  it("resets retry counter upon successful open", () => {
    const queryClient = new QueryClient();
    const manager = new SSEManager(queryClient, { maxRetries: 4 });

    manager.start();
    // Fail 1
    mockEventSourceInstances[0]!.onerror?.(new Event("error"));
    vi.advanceTimersByTime(2000);
    expect(mockEventSourceInstances.length).toBe(2);

    // Success on attempt 2
    mockEventSourceInstances[1]!.onopen?.();
    expect(manager.getRetryCount()).toBe(0);

    manager.stop();
  });

  it("allows a visibility recovery after the circuit breaker trips", () => {
    const queryClient = new QueryClient();
    const manager = new SSEManager(queryClient, { maxRetries: 1 });

    manager.start();
    mockEventSourceInstances[0]!.onerror?.(new Event("error"));
    expect(mockEventSourceInstances).toHaveLength(1);

    Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
    document.dispatchEvent(new Event("visibilitychange"));

    expect(mockEventSourceInstances).toHaveLength(2);
    manager.stop();
  });

  it("dispatches incoming events to debounced invalidator", () => {
    const queryClient = new QueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries").mockResolvedValue();
    const manager = new SSEManager(queryClient, { debounceMs: 150 });

    manager.start();
    const es = mockEventSourceInstances[0];

    const payload: SSEEventPayload = {
      id: "evt-1",
      topic: "events.created",
      version: 1,
      occurred_at: new Date().toISOString(),
      resource: "event",
      resource_id: "row-1",
    };

    es!.emit("events.created", payload);
    vi.advanceTimersByTime(160);

    expect(invalidateSpy).toHaveBeenCalled();
    manager.stop();
  });
});
