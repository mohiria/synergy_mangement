-- +goose Up
-- O／KR／任务的持久编号序号（AC-64、F1）：编号在创建时分配、之后不再变动，
-- 删除同级对象也不重排——用户会把「1.1.1」当标识符在会议和讨论里引用。
-- 只存各级序号，展示编号（O1 / KR1.1 / 1.1.1）由序号链在 domain 派生。
ALTER TABLE objectives   ADD COLUMN code_seq INT NOT NULL DEFAULT 0;
ALTER TABLE key_results  ADD COLUMN code_seq INT NOT NULL DEFAULT 0;
ALTER TABLE tasks        ADD COLUMN code_seq INT NOT NULL DEFAULT 0;

-- 存量数据按既有排序回填一次，回填后编号即固定。
UPDATE objectives o SET code_seq = s.n
FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY project_id ORDER BY sort_order, id) AS n FROM objectives) s
WHERE o.id = s.id;

UPDATE key_results k SET code_seq = s.n
FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY objective_id ORDER BY sort_order, id) AS n FROM key_results) s
WHERE k.id = s.id;

UPDATE tasks t SET code_seq = s.n
FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY key_result_id ORDER BY id) AS n FROM tasks) s
WHERE t.id = s.id;

-- +goose Down
ALTER TABLE objectives  DROP COLUMN code_seq;
ALTER TABLE key_results DROP COLUMN code_seq;
ALTER TABLE tasks       DROP COLUMN code_seq;
