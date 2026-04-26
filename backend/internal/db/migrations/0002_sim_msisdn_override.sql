-- 0002_sim_msisdn_override.sql
-- SIM 本地显示号码覆盖值；硬件 OwnNumbers 仍保存在 sims.msisdn。

ALTER TABLE sims ADD COLUMN msisdn_override TEXT;
