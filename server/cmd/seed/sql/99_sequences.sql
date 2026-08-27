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
