-- 0002_modem_nickname.sql
-- 同步副本，与 backend/internal/db/migrations/0002_modem_nickname.sql 保持一致。
-- 当前运行时仅 internal/db/migrations 生效（embed.FS），此文件为文档/手工参考。

ALTER TABLE modems ADD COLUMN nickname TEXT;
