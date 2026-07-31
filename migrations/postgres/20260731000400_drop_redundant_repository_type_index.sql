-- (type, last_synced_at) 复合索引的左前缀已覆盖 type 等值过滤，单列索引冗余。
DROP INDEX "repository_type";
