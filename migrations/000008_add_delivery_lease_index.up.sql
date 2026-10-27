-- ClaimDeliveries 的第二条腿（租约过期的 sending 行）在只有
-- idx_message_deliveries_claim (status, next_attempt_at) 时无法用索引：
-- status=1 会把所有在途行都扫一遍再按 locked_until 过滤。真实 PG 16 上
-- 25 万行 / 5 万在途时计划退化成 Parallel Seq Scan（7.6ms，扫 25 万行）；
-- 加上这个部分索引后是 Bitmap Index Scan（1.2ms，只读 2500 行）。
-- 部分索引在 SQLite 同样支持，测试库不需要它也能跑（AutoMigrate 不建索引）。
CREATE INDEX IF NOT EXISTS idx_message_deliveries_lease
    ON message_deliveries (locked_until)
    WHERE status = 1;
