-- PurgeTerminalDeliveries（internal/message/repository.go）按
-- "status IN (2,4,5) AND updated_at < cutoff" 删除终态投递记录。
-- 现有索引是 (status, next_attempt_at) 和 (locked_until) WHERE status = 1，
-- 都不覆盖 updated_at：sent 行只增不减，清理扫描的代价会随保留窗口内的
-- 数据量线性变差（每次 janitor tick 都扫一遍全部终态行）。
--
-- 用部分索引只收终态行：pending/sending/failed 永远不会被清理，
-- 没必要为它们维护索引项。谓词必须与 repository 里的 Delete 保持一致。
--
-- 真实 PG 16、20 万行 / 47 MB、模拟每小时清理一次（窗口 30 天，待删约 1800 行）：
--   无索引：Seq Scan，31.1 ms，扫全表 20 万行；
--   有索引：Index Scan，0.5 ms，只读窗口内的行（约 63x）。
-- 待删比例越低、表越大，差距越大。改 PurgeTerminalDeliveries 的谓词时同步复核。
CREATE INDEX IF NOT EXISTS idx_message_deliveries_purge
    ON message_deliveries (updated_at)
    WHERE status IN (2, 4, 5);
