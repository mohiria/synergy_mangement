-- 显式写过 id 的表要把 identity 序列推到 max(id)，否则后续插入会撞主键。
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'users', 'projects', 'objectives', 'key_results', 'tasks', 'pool_reviews',
        'task_receipts', 'task_invites', 'deliverables', 'deliverable_files',
        'deliverable_edges', 'input_requests', 'completion_reviews',
        'field_change_requests', 'artifact_packages', 'remind_logs',
        'discussions', 'notifications', 'task_activities'
    ] LOOP
        EXECUTE format(
            'SELECT setval(pg_get_serial_sequence(%L, ''id''), COALESCE((SELECT max(id) FROM %I), 1))',
            t, t);
    END LOOP;
END $$;

-- O／KR／任务的持久编号序号（AC-64）：种子按 id 显式插入，没写 code_seq，
-- 而回填只在迁移 00031 里跑过一次——种子重建后编号会全部落空。
-- 这里按与迁移相同的口径补一次，让演示数据里的 O1／KR1.1／1.1.1 与真实使用一致。
UPDATE objectives o SET code_seq = s.n
FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY project_id ORDER BY sort_order, id) AS n FROM objectives) s
WHERE o.id = s.id;

UPDATE key_results k SET code_seq = s.n
FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY objective_id ORDER BY sort_order, id) AS n FROM key_results) s
WHERE k.id = s.id;

UPDATE tasks t SET code_seq = s.n
FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY key_result_id ORDER BY id) AS n FROM tasks) s
WHERE t.id = s.id;
