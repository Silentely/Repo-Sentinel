import { useEffect } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";

export interface SSEEventPayload {
  id: string;
  topic: string;
  version: number;
  occurred_at: string;
  resource: string;
  resource_id: string;
}

export interface DebouncedInvalidator {
  schedule: (topic: string) => void;
  flush: () => void;
  cancel: () => void;
}

export interface SSEManagerOptions {
  url?: string;
  maxRetries?: number;
  debounceMs?: number;
  onCircuitBreak?: () => void;
}

/**
 * Creates a debounced invalidator that coalesces high-frequency SSE events (e.g. 50 events in 100ms)
 * into a single batch of query invalidations, protecting browser render loops and network.
 */
export function createDebouncedInvalidator(
  queryClient: QueryClient,
  debounceMs = 150
): DebouncedInvalidator {
  let timer: ReturnType<typeof setTimeout> | null = null;
  const pendingDomains = new Set<string>();

  const flush = () => {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
    if (pendingDomains.size === 0) return;

    const domains = Array.from(pendingDomains);
    pendingDomains.clear();

    for (const domain of domains) {
      queryClient.invalidateQueries({
        queryKey: [domain],
        refetchType: "active",
      });
    }
  };

  const schedule = (topic: string) => {
    // Every event updates dashboard counters
    pendingDomains.add("dashboard");

    switch (topic) {
      case "events.created":
        pendingDomains.add("events");
        break;
      case "outbox.changed":
        pendingDomains.add("outbox");
        break;
      case "ai_review.changed":
        pendingDomains.add("work-items");
        pendingDomains.add("pull-requests");
        break;
      default:
        break;
    }

    if (!timer) {
      timer = setTimeout(flush, debounceMs);
    }
  };

  const cancel = () => {
    if (timer) clearTimeout(timer);
    timer = null;
    pendingDomains.clear();
  };

  return { schedule, flush, cancel };
}

/**
 * SSEManager manages the SSE lifecycle with:
 * 1. Exponential backoff retry with max consecutive failure limit (circuit breaker)
 * 2. Tab visibility recovery
 * 3. Batched/debounced query invalidation
 */
export class SSEManager {
  private queryClient: QueryClient;
  private url: string;
  private maxRetries: number;
  private onCircuitBreak?: () => void;
  private invalidator: DebouncedInvalidator;

  private es: EventSource | null = null;
  private retryCount = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private isStopped = false;
  private isCircuitBroken = false;
  private visibilityHandler: (() => void) | null = null;

  constructor(queryClient: QueryClient, options: SSEManagerOptions = {}) {
    this.queryClient = queryClient;
    this.url = options.url || "/api/v1/events/stream";
    this.maxRetries = options.maxRetries ?? 4;
    this.onCircuitBreak = options.onCircuitBreak;
    this.invalidator = createDebouncedInvalidator(
      queryClient,
      options.debounceMs ?? 150
    );
  }

  public start() {
    this.isStopped = false;
    this.isCircuitBroken = false;
    this.connect();

    if (typeof document !== "undefined") {
      this.visibilityHandler = () => {
        if (document.visibilityState === "visible") {
          // Invalidate active queries upon returning to tab
          this.queryClient.invalidateQueries({ refetchType: "active" });

          // Re-establish connection if dropped and circuit breaker not tripped
          if (!this.isStopped && (!this.es || this.es.readyState === 2)) {
            this.isCircuitBroken = false;
            this.retryCount = 0;
            this.connect();
          }
        }
      };
      document.addEventListener("visibilitychange", this.visibilityHandler);
    }
  }

  public stop() {
    this.isStopped = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.es) {
      this.es.close();
      this.es = null;
    }
    if (this.visibilityHandler && typeof document !== "undefined") {
      document.removeEventListener("visibilitychange", this.visibilityHandler);
      this.visibilityHandler = null;
    }
    this.invalidator.cancel();
  }

  public getRetryCount(): number {
    return this.retryCount;
  }

  private connect() {
    if (this.isStopped || this.isCircuitBroken) return;

    if (this.es) {
      this.es.close();
      this.es = null;
    }

    try {
      const es = new EventSource(this.url, { withCredentials: true });
      this.es = es;

      es.onopen = () => {
        this.retryCount = 0;
      };

      es.onerror = () => {
        if (this.isStopped) return;

        es.close();
        this.es = null;

        this.retryCount++;
        if (this.retryCount >= this.maxRetries) {
          this.isCircuitBroken = true;
          this.onCircuitBreak?.();
          return;
        }

        // Exponential backoff: 1s, 2s, 4s, 8s... max 30s
        const delay = Math.min(1000 * Math.pow(2, this.retryCount - 1), 30000);
        this.reconnectTimer = setTimeout(() => {
          this.connect();
        }, delay);
      };

      const handleEvent = (topic: string) => () => {
        try {
          this.invalidator.schedule(topic);
        } catch {
          // Ignore parsing or handling errors
        }
      };

      es.addEventListener("events.created", handleEvent("events.created"));
      es.addEventListener("outbox.changed", handleEvent("outbox.changed"));
      es.addEventListener("ai_review.changed", handleEvent("ai_review.changed"));
    } catch {
      // EventSource instantiation error
    }
  }
}

/**
 * React hook that connects to the server SSE event stream and automatically
 * invalidates TanStack Query caches with debouncing when events arrive.
 */
export function useLiveEventStream(options?: SSEManagerOptions): void {
  const queryClient = useQueryClient();

  useEffect(() => {
    const manager = new SSEManager(queryClient, options);
    manager.start();

    return () => {
      manager.stop();
    };
  }, [queryClient, options]);
}
