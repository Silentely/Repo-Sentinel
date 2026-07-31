-- 持久化调度队列为无人使用的死表（调度由内存 ticker 实现，见 internal/syncx/scheduler.go）。
DROP TABLE "scheduled_jobs";
