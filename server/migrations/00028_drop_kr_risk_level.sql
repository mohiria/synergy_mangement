-- +goose Up
-- 风险等级改为读时派生（PRD §5.7、§0.7 Q1 裁决）：不落库、无写入路径。
-- 原列从未有 UPDATE 路径，真实系统只可能停在 'normal'，直接丢弃不损失事实。
ALTER TABLE key_results DROP COLUMN risk_level;

-- +goose Down
ALTER TABLE key_results ADD COLUMN risk_level TEXT NOT NULL DEFAULT 'normal';
