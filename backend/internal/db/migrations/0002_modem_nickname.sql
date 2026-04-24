-- 0002_modem_nickname.sql
-- 为 modems 表添加 nickname 列，支持用户给模块打备注。
-- 空字符串 / NULL 都视作"未设置"，前端显示回退到 model/device_id。

ALTER TABLE modems ADD COLUMN nickname TEXT;
